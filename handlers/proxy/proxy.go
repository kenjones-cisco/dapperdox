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

// Package proxy provides a proxy redirector for APIs.
package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/gorilla/mux"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/logger"
)

type responseCapture struct {
	http.ResponseWriter

	statusCode int
}

func (r *responseCapture) WriteHeader(status int) {
	r.statusCode = status
	r.ResponseWriter.WriteHeader(status)
}

// Register handles registering paths to proxy.
func Register(r *mux.Router) {
	RegisterWithConfig(r, config.Snapshot())
}

// RegisterWithConfig registers configured API proxies.
func RegisterWithConfig(r *mux.Router, settings config.Settings) {
	log().Debug("Registering proxied paths:")

	for k, v := range settings.ProxyPath {
		register(r, k, v)
	}

	log().Debug("Registering proxied paths done.")
}

func register(rtr *mux.Router, routePattern, target string) {
	u, _ := url.Parse(target)

	log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("+ %s -> %s", routePattern, target))

	proxy := &httputil.ReverseProxy{Rewrite: func(request *httputil.ProxyRequest) {
		request.SetURL(u)
		request.SetXForwarded()
		r := request.Out

		scheme := "http://"
		if request.In.TLS != nil {
			scheme = "https://"
		}

		log().DebugContext(r.Context(), fmt.Sprintf("Proxy request to: %s%s%s", scheme, r.Host, r.URL.Path))
	}}

	rtr.PathPrefix(routePattern).HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := &responseCapture{w, 0}
		s := time.Now()
		log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("Proxy request started: %v", s))

		proxy.ServeHTTP(rc, r)

		e := time.Now()
		log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("Proxy request completed: %v", e))

		log().InfoContext(r.Context(), fmt.Sprintf("PROXY %s %s (%d, %v)", r.Method, r.URL.Path, rc.statusCode, e.Sub(s)))
	})
}
