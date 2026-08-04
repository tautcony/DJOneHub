package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"syscall"

	"github.com/iniwex5/vohive/internal/app"
)

func main() {
	// The macOS native UI (AppKit/SwiftUI) must run on the process main
	// thread. Pin it before any goroutine scheduling so bridge.Start can
	// block here safely while the HTTP server runs on worker goroutines.
	goruntime.LockOSThread()

	listen := flag.String("listen", defaultListenAddress(), "HTTP listen address")
	webDir := flag.String("web-dir", defaultWebDirectory(), "Vue static asset directory")
	demo := flag.Bool("demo", false, "run without hardware")
	flag.Parse()
	var instance *app.App
	var err error
	if *demo {
		instance, err = app.NewOffline()
	} else {
		instance, err = app.New()
	}
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	instance.Start(ctx)
	server := &http.Server{Addr: *listen, Handler: withVueAssets(instance.HTTP.Handler(), *webDir)}
	shutdown := func() {
		_ = server.Shutdown(context.Background())
		instance.Stop()
	}
	if instance.NativeUI.HasUI() {
		// One process, one UI: serve HTTP on goroutines and run the native UI
		// on the main thread. Exiting the UI (menu bar quit) exits the app.
		go func() {
			<-ctx.Done()
			instance.NativeUI.Stop()
			shutdown()
		}()
		log.Printf("DJOneHub listening on http://%s", *listen)
		go func() {
			if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatal(err)
			}
		}()
		_ = instance.NativeUI.Start(ctx, fmt.Sprintf("http://%s/", *listen))
		stop()
		shutdown()
		return
	}
	go func() {
		<-ctx.Done()
		shutdown()
	}()
	log.Printf("DJOneHub listening on http://%s", *listen)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func defaultListenAddress() string { return "127.0.0.1:7575" }

func defaultWebDirectory() string {
	executable, err := os.Executable()
	if err == nil {
		bundleWeb := filepath.Clean(filepath.Join(filepath.Dir(executable), "..", "Resources", "web", "dist"))
		if _, err := os.Stat(bundleWeb); err == nil {
			return bundleWeb
		}
	}
	return "web/dist"
}

func withVueAssets(apiHandler http.Handler, webDir string) http.Handler {
	assets := http.FileServer(http.Dir(webDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			apiHandler.ServeHTTP(w, r)
			return
		}
		assetPath := filepath.Join(webDir, filepath.Clean("/"+r.URL.Path))
		if _, err := os.Stat(assetPath); os.IsNotExist(err) {
			http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
			return
		}
		assets.ServeHTTP(w, r)
	})
}
