package main

import (
	"log"
	"net/http"

	"github.com/josh/little-sfu/internal/signaling"
)

func main() {
	server := signaling.NewServer()

	serveMux := http.NewServeMux()

	serveMux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "internal/web/index.html")
	})

	serveMux.HandleFunc("POST /publish/{room}", server.PublishHandler)

	log.Println("Little SFU listening on http://localhost:8080")

	if err := http.ListenAndServe(":8080", serveMux); err != nil {
		log.Fatal(err)
	}
}
