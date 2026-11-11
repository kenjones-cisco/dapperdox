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
	"log/slog"
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

var errManagementAddress = errors.New("management address must use a loopback IP")

func main() {
	if err := run(); err != nil {
		log.Logger().Error("documentation server failed", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
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

	settings, loadErr := config.Load()
	if loadErr != nil {
		return loadErr
	}

	if err := log.SetLevel(settings.LogLevel); err != nil {
		return err
	}

	var chain http.Handler

	var management http.Handler

	if settings.DiscoveryEnabled {
		// initialize auto-discovery instance
		discoverer, err := discover.NewDiscovererWithConfig(settings)
		if err != nil {
			return fmt.Errorf("create discoverer: %w", err)
		}

		// initialize updater instance
		updater, err := handlers.NewAutoDiscoverUpdaterWithConfig(discoverer, settings)
		if err != nil {
			return fmt.Errorf("initialize documentation: %w", err)
		}

		defer updater.Close()
		defer discoverer.Shutdown()

		go discoverer.Run()

		chain = updater.Router()
		management = updater.ManagementHandler()
	} else {
		var err error

		chain, err = handlers.NewRouterChainWithConfig(settings)
		if err != nil {
			return fmt.Errorf("initialize documentation: %w", err)
		}

		management = http.HandlerFunc(staticStatus)
	}

	var (
		listener net.Listener
		err      error
	)

	if settings.TLSCert != "" && settings.TLSKey != "" {
		listener, err = network.NewSecuredListenerWithConfig(settings)
	} else {
		listener, err = network.NewListenerWithConfig(settings)
	}

	if err != nil {
		return fmt.Errorf("listen on %s: %w", settings.BindAddr, err)
	}

	return serveDocumentationWithConfig(listener, chain, management, settings)
}

func staticStatus(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/status" || r.Method != http.MethodGet {
		http.NotFound(w, r)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	if err := json.NewEncoder(w).Encode(map[string]bool{"ready": true}); err != nil {
		log.Logger().ErrorContext(r.Context(), "encode static documentation status", slog.Any("error", err))
	}
}

func serveDocumentationWithConfig(listener net.Listener, chain, management http.Handler, settings config.Settings) error {
	managementListener, err := listenManagement(settings.ManagementAddr)
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
			log.Logger().With(slog.Any("error", err)).Error("documentation listener failed")
		}
	case <-ctx.Done():
	}

	shutdown, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	for _, s := range []*http.Server{server, operator} {
		if shutdownErr := s.Shutdown(shutdown); shutdownErr != nil {
			log.Logger().With(slog.Any("error", shutdownErr)).Error("shutdown documentation listener")
			err = errors.Join(err, shutdownErr)

			if closeErr := s.Close(); closeErr != nil {
				log.Logger().With(slog.Any("error", closeErr)).Error("close documentation listener")
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
		return nil, errManagementAddress
	}

	return new(net.ListenConfig).Listen(context.Background(), "tcp", address)
}
