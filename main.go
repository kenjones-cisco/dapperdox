/*
Copyright (C) 2016-2017 dapperdox.com

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/discover"
	"github.com/kenjones-cisco/dapperdox/handlers"
	log "github.com/kenjones-cisco/dapperdox/logger"
	"github.com/kenjones-cisco/dapperdox/network"
	"github.com/kenjones-cisco/dapperdox/version"
)

const (
	headerTimeout   = 5 * time.Second
	shutdownTimeout = 10 * time.Second
	listenerCount   = 2
)

func main() {
	pflag.Usage = func() {
		_, _ = fmt.Fprint(os.Stderr, "Usage:\n")
		_, _ = fmt.Fprintf(os.Stderr, "  %s [OPTIONS]\n\n", version.ShortName)
		_, _ = fmt.Fprintf(os.Stderr, "%s\n\n", version.ProductName)
		_, _ = fmt.Fprintln(os.Stderr, pflag.CommandLine.FlagUsages())
	}
	// parse the CLI flags
	pflag.Parse()
	_ = viper.BindPFlags(pflag.CommandLine)

	if viper.GetBool(config.Version) {
		fmt.Print(version.GetVersionDisplay())
		os.Exit(0)
	}

	config.Init()

	log.SetLevel(viper.GetString(config.LogLevel))

	var chain http.Handler

	var management http.Handler

	if viper.GetBool(config.DiscoveryEnabled) {
		// initialize auto-discovery instance
		discoverer, err := discover.NewDiscoverer()
		if err != nil {
			log.Logger().Fatalf("unable to create discoverer: %v", err)
		}

		// initialize updater instance
		updater, err := handlers.NewAutoDiscoverUpdater(discoverer)
		if err != nil {
			log.Logger().Fatalf("initialize documentation: %v", err)
		}

		defer updater.Close()
		defer discoverer.Shutdown()

		go discoverer.Run()

		chain = updater.Router()
		management = updater.ManagementHandler()
	} else {
		var err error

		chain, err = handlers.NewRouterChain()
		if err != nil {
			log.Logger().Fatalf("initialize documentation: %v", err)
		}

		management = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/status" || r.Method != http.MethodGet {
				http.NotFound(w, r)

				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")

			if err := json.NewEncoder(w).Encode(map[string]bool{"ready": true}); err != nil {
				log.Logger().WithError(err).Error("encode static documentation status")
			}
		})
	}

	var listener net.Listener

	var err error

	if viper.GetString(config.TLSCert) != "" && viper.GetString(config.TLSKey) != "" {
		listener, err = network.NewSecuredListener()
	} else {
		listener, err = network.NewListener()
	}

	if err != nil {
		log.Logger().Fatalf("Error listening on %s: %s", viper.GetString(config.BindAddr), err)
	}

	err = serveDocumentation(listener, chain, management)
	if err != nil {
		log.Logger().Fatalf("documentation server: %v", err)
	}
}

func serveDocumentation(listener net.Listener, chain, management http.Handler) error {
	managementListener, err := listenManagement(viper.GetString(config.ManagementAddr))
	if err != nil {
		_ = listener.Close()

		return fmt.Errorf("listen for operator status: %w", err)
	}

	server := &http.Server{Handler: chain, ReadHeaderTimeout: headerTimeout}
	operator := &http.Server{Handler: management, ReadHeaderTimeout: headerTimeout}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	results := make(chan error, listenerCount)

	go func() { results <- server.Serve(listener) }()
	go func() { results <- operator.Serve(managementListener) }()
	select {
	case err = <-results:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		} else {
			log.Logger().WithError(err).Error("documentation listener failed")
		}
	case <-ctx.Done():
	}

	shutdown, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	for _, s := range []*http.Server{server, operator} {
		if shutdownErr := s.Shutdown(shutdown); shutdownErr != nil {
			log.Logger().WithError(shutdownErr).Error("shutdown documentation listener")
			err = errors.Join(err, shutdownErr)

			if closeErr := s.Close(); closeErr != nil {
				log.Logger().WithError(closeErr).Error("close documentation listener")
				err = errors.Join(err, closeErr)
			}
		}
	}

	return err
}

func listenManagement(address string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return nil, errors.New("management address must use a loopback IP")
	}

	return net.Listen("tcp", address)
}
