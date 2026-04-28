package main

import (
	"log"
	"net/http"
	"time"
)

func main() {
	mux := http.NewServeMux()

	server := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	mux.Handle("/", http.FileServer(http.Dir(".")))
	mux.Handle("/logo.png", http.FileServer(http.Dir("./assets")))
	log.Fatal(server.ListenAndServe())
}
