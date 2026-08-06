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
	"sync"
	"syscall"
	"time"

	"github.com/iniwex5/vohive/internal/api/http"
	"github.com/iniwex5/vohive/internal/app"
	"github.com/iniwex5/vohive/pkg/logger"
)

const (
	// httpDrainTimeout bounds the HTTP drain; the worker join below uses its
	// own separate deadline so a slow handler cannot starve the workers.
	httpDrainTimeout = 5 * time.Second
	// workerStopTimeout bounds the reverse-order worker join inside
	// instance.Stop (notification sink drain, pollers, runtime).
	workerStopTimeout = 10 * time.Second
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
	// Initialize the structured logger before anything else, so startup
	// failures and legacy log.Printf call sites share one output format
	// instead of mixing standard-log lines into the zap output.
	logger.Setup(logger.LogConfig{Filename: logger.DefaultFilename("DJOneHub")})
	// The temporary loopback boundary rejects wildcard, non-loopback, and
	// hostname listen addresses before any application or UI work starts.
	port, err := validateListenAddress(*listen)
	if err != nil {
		logger.Fatal(err.Error())
	}
	var instance *app.App
	if *demo {
		instance, err = app.NewOffline()
	} else {
		instance, err = app.New()
	}
	if err != nil {
		logger.Fatal(err.Error())
	}
	// Anchor the HTTP boundary's Origin/Host checks to the validated loopback
	// port; without this every state-changing request is rejected.
	instance.HTTP.SetLoopbackPort(port)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	instance.Start(ctx)

	// Bind the listener before the native UI starts, so a bind failure is
	// detected before the UI is launched against an unreachable URL.
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		logger.Fatal("DJOneHub failed to listen", "listen", *listen, "error", err)
	}
	server := &http.Server{Addr: *listen, Handler: withVueAssets(instance.HTTP.Handler(), *webDir)}

	// The one bounded shutdown sequence, run exactly once: close shutdown
	// admission before draining the HTTP server (separate deadline), then
	// stop and join the workers (separate deadline). The native UI is kept
	// alive until the notification sink calls have returned; the caller stops
	// it afterwards.
	var shutdownOnce sync.Once
	shutdown := func() {
		shutdownOnce.Do(func() {
			instance.BeginShutdown()
			drainCtx, drainCancel := context.WithTimeout(context.Background(), httpDrainTimeout)
			defer drainCancel()
			_ = server.Shutdown(drainCtx)
			workerCtx, workerCancel := context.WithTimeout(context.Background(), workerStopTimeout)
			defer workerCancel()
			if err := instance.Stop(workerCtx); err != nil {
				log.Printf("shutdown: %v", err)
			}
		})
	}

	if instance.NativeUI.HasUI() {
		// One process, one UI: serve HTTP on goroutines and run the native UI
		// on the main thread. UI exit (menu bar quit) and signal-driven
		// shutdown converge on the single goroutine below, so the shutdown
		// sequence can never run twice or in parallel.
		uiExited := make(chan struct{})
		serveErr := make(chan error, 1)
		shutdownDone := make(chan struct{})
		go func() {
			defer close(shutdownDone)
			uiRunning := false
			select {
			case <-ctx.Done():
				// Signal-driven: the UI is still running and must be stopped
				// after the sink drain.
				uiRunning = true
			case <-uiExited:
				// Menu-bar quit: the AppKit run loop has already exited.
			case serveErrValue := <-serveErr:
				log.Printf("http serve error: %v", serveErrValue)
				uiRunning = true
			}
			shutdown()
			if uiRunning {
				// Stop the UI only after the sink queue has been drained, so
				// the last notification calls reach a live run loop.
				instance.NativeUI.Stop()
			}
		}()
		log.Printf("DJOneHub listening on http://%s", *listen)
		go func() {
			if serveErrValue := server.Serve(listener); serveErrValue != nil && !errors.Is(serveErrValue, http.ErrServerClosed) {
				serveErr <- serveErrValue
			}
		}()
		_ = instance.NativeUI.Start(ctx, fmt.Sprintf("http://%s/", *listen))
		close(uiExited)
		stop()
		<-shutdownDone
		return
	}
	serveErr := make(chan error, 1)
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
		case serveErrValue := <-serveErr:
			log.Printf("http serve error: %v", serveErrValue)
		}
		shutdown()
	}()
	log.Printf("DJOneHub listening on http://%s", *listen)
	if serveErrValue := server.Serve(listener); serveErrValue != nil && !errors.Is(serveErrValue, http.ErrServerClosed) {
		// Serve failed on its own: the goroutine above runs the normal
		// shutdown path; wait for it and exit with the failure.
		log.Printf("http serve error: %v", serveErrValue)
		stop()
		<-shutdownDone
		os.Exit(1)
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
