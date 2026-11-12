package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/handlers"
)

func main() {
	var lc net.ListenConfig

	listener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:32179")
	if err != nil {
		log.Fatal(err)
	}

	assets, theme := os.Getenv("BROWSER_TEST_ASSETS"), os.Getenv("BROWSER_TEST_THEME")
	if assets == "" {
		assets = "fixtures/compatibility/assets"
	}

	if theme == "" {
		theme = "default"
	}

	router, err := handlers.NewRouterChainWithConfig(config.Settings{
		DefaultAssetsDir: "assets",
		AssetsDir:        assets,
		Theme:            theme,
		SpecDir:          "fixtures/compatibility",
		SpecFilename:     []string{"swagger.json"},
		SpecDefaultHost:  listener.Addr().String(),
		ForceSpecList:    true,
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
		_, _ = w.Write([]byte("fixture\x00\xff\x80download\n"))
	})
	mux.HandleFunc("/fixture-api/upload", fixtureUpload)
	mux.Handle("/", router)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: time.Second}
	log.Fatal(server.Serve(listener))
}

func fixtureUpload(w http.ResponseWriter, r *http.Request) {
	const maxUploadBytes = 1024 * 1024

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)

	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	var caption, filename string

	var attachment []byte

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}

		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		body, err := io.ReadAll(part)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		switch part.FormName() {
		case "caption":
			caption = string(body)
		case "attachment":
			filename, attachment = part.FileName(), body
		}
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(map[string]any{
		"caption": caption, "filename": filename, "body": attachment,
	}); err != nil {
		log.Print(err)
	}
}
