package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/iniwex5/vohive/internal/api/http"
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
	// The temporary loopback boundary rejects wildcard, non-loopback, and
	// hostname listen addresses before any application or UI work starts.
	port, err := validateListenAddress(*listen)
	if err != nil {
		log.Fatal(err)
	}
	var instance *app.App
	if *demo {
		instance, err = app.NewOffline()
	} else {
		instance, err = app.New()
	}
	if err != nil {
		log.Fatal(err)
	}
	// Anchor the HTTP boundary's Origin/Host checks to the validated loopback
	// port; without this every state-changing request is rejected.
	instance.HTTP.SetLoopbackPort(port)
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

// validateListenAddress enforces the temporary loopback-only boundary: the
// host must be 127.0.0.1, localhost, or [::1] and the port must be explicit
// and valid. Wildcard, non-loopback, and hostname addresses fail before any
// application or UI startup.
func validateListenAddress(listen string) (int, error) {
	host, portText, err := net.SplitHostPort(listen)
	if err != nil {
		return 0, fmt.Errorf("listen address %q is invalid: %w", listen, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("listen address %q must include an explicit port", listen)
	}
	if !httpapi.IsLoopbackHost(host) {
		return 0, fmt.Errorf("listen address %q must be a loopback address (127.0.0.1, localhost, or [::1])", listen)
	}
	return port, nil
}

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
