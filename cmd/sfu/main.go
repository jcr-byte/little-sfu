package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/josh/little-sfu/internal/signaling"
)

func main() {
	server := signaling.NewServer()
	defer server.Close()

	serveMux := http.NewServeMux()

	serveMux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "internal/web/index.html")
	})

	serveMux.HandleFunc("POST /publish/{room}", server.PublishHandler)
	serveMux.HandleFunc("POST /watch/{room}", server.WatchHandler)

	httpServer := &http.Server{
		Addr:    ":8080",
		Handler: serveMux,
	}
	listener, err := net.Listen("tcp", httpServer.Addr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", httpServer.Addr, err)
	}

	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- httpServer.Serve(listener)
	}()

	signals, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	log.Println("Little SFU listening on http://localhost:8080")

	select {
	case err := <-serveErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP server stopped: %v", err)
		}
		return

	case <-signals.Done():
		log.Println("Shutting down")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("Graceful HTTP shutdown failed: %v", err)
		if closeErr := httpServer.Close(); closeErr != nil {
			log.Printf("Closing HTTP server: %v", closeErr)
		}
	}

	<-serveErrors
}
