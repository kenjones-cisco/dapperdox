package main

import (
	"context"
	"fmt"
	"log"
	"mime"
	"net"
	"net/http"
	"time"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/handlers"
)

func main() {
	// Keep static JavaScript registration independent of the image's MIME database.
	if err := mime.AddExtensionType(".js", "application/javascript"); err != nil {
		log.Fatal(err)
	}

	var lc net.ListenConfig

	listener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:32179")
	if err != nil {
		log.Fatal(err)
	}

	router, err := handlers.NewRouterChainWithConfig(config.Settings{
		DefaultAssetsDir: "assets",
		AssetsDir:        "fixtures/compatibility/assets",
		Theme:            "default",
		SpecDir:          "fixtures/compatibility",
		SpecFilename:     []string{"swagger.json"},
		SpecDefaultHost:  listener.Addr().String(),
	})
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/fixture-api/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"accepted":true}`)
	})
	mux.HandleFunc("/fixture-api/download", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("fixture\x00download\n"))
	})
	mux.Handle("/", router)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: time.Second}
	log.Fatal(server.Serve(listener))
}
