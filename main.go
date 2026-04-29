package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

type apiConfig struct {
	fileserverHits atomic.Int32
}

func main() {
	mux := http.NewServeMux()

	server := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	cfg := apiConfig{fileserverHits: atomic.Int32{}}

	mux.Handle("/app/", (&cfg).middlewareMetricsInc(http.StripPrefix("/app/", http.FileServer(http.Dir(".")))))
	mux.Handle("/app/logo.png", (&cfg).middlewareMetricsInc(http.StripPrefix("/app/", http.FileServer(http.Dir("./assets")))))

	mux.HandleFunc("GET /api/healthz", handlerReadiness)
	mux.HandleFunc("POST /api/validate_chirp", handlerFirstJson)
	mux.HandleFunc("GET /admin/metrics", (&cfg).handlerCountRequests)
	mux.HandleFunc("POST /admin/reset", (&cfg).handlerResetRequestCount)

	log.Fatal(server.ListenAndServe())
}

// middleware method that wrap handler function to increment fileserverHits
func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("middleware hit: %s", r.URL.Path)
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func handlerFirstJson(w http.ResponseWriter, r *http.Request) {
	type requestJson struct {
		Body string `json:"body"`
	}
	type errorJson struct {
		Error string `json:"error"`
	}
	type returnJson struct {
		Valid bool `json:"valid"`
	}

	// this method will returns json either normal, or error json
	w.Header().Set("Content-Type", "application/json")

	decoder := json.NewDecoder(r.Body)
	req := requestJson{}
	err := decoder.Decode(&req)
	if err != nil {
		resp := errorJson{
			Error: "Something went wrong",
		}
		dat, err := json.Marshal(resp)
		if err != nil {
			log.Printf("Error marshalling JSON: %s", err)
			w.WriteHeader(500)
			return
		}

		log.Printf("Error decoding parameters: %s", err)
		w.WriteHeader(500)
		w.Write(dat)
		return
	}

	if len(req.Body) > 140 {
		resp := errorJson{
			Error: "Chirp is too long",
		}
		dat, err := json.Marshal(resp)
		if err != nil {
			log.Printf("Error marshalling JSON: %s", err)
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(400)
		w.Write(dat)
		return
	}

	resp := returnJson{
		Valid: true,
	}
	dat, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Error marshalling JSON: %s", err)
		w.WriteHeader(500)
		return
	}
	w.WriteHeader(200)
	w.Write(dat)
}

func handlerReadiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(200)
	if _, err := w.Write([]byte("OK\n")); err != nil {
		log.Fatal(errors.New("Error writing into response body!"))
	}
}

func (cfg *apiConfig) handlerCountRequests(w http.ResponseWriter, r *http.Request) {
	// Set response content type to text/html
	w.Header().Add("Content-Type", "text/html")
	if _, err := w.Write([]byte(fmt.Sprintf(`<html><body><h1>Welcome, Chirpy Admin</h1><p>Chirpy has been visited %d times!</p></body></html>
`, cfg.fileserverHits.Load()))); err != nil {
		log.Fatal(err)
	}
}

func (cfg *apiConfig) handlerResetRequestCount(w http.ResponseWriter, r *http.Request) {
	cfg.fileserverHits.Store(0)

}
