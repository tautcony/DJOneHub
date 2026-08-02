package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os/signal"
	"strings"
	"syscall"

	"github.com/iniwex5/vohive/internal/app"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:7576", "HTTP listen address")
	webDir := flag.String("web-dir", "web/dist", "Vue static asset directory")
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
	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
		instance.Stop()
	}()
	log.Printf("DJOneHub listening on http://%s", *listen)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func withVueAssets(apiHandler http.Handler, webDir string) http.Handler {
	assets := http.FileServer(http.Dir(webDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			apiHandler.ServeHTTP(w, r)
			return
		}
		assets.ServeHTTP(w, r)
	})
}
