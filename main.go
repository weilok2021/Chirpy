package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
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
	mux.HandleFunc("POST /api/validate_chirp", handlerValidateChirp)
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

func handlerValidateChirp(w http.ResponseWriter, r *http.Request) {
	type requestJson struct {
		Body string `json:"body"`
	}
	type returnJson struct {
		CleanedBody string `json:"cleaned_body"`
	}

	// this method will returns json either normal, or error json
	w.Header().Set("Content-Type", "application/json")

	decoder := json.NewDecoder(r.Body)
	req := requestJson{}
	err := decoder.Decode(&req)
	if err != nil {
		respondWithError(w, 500, "Something went wrong")
		return
	}

	if len(req.Body) > 140 {
		respondWithError(w, 400, "Chirp is too long")
		return
	}

	formatted_body := replaceProfane(req.Body)
	resp := returnJson{
		CleanedBody: formatted_body,
	}
	responseWithJson(w, 200, resp)
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

// helper functions
func replaceProfane(text string) string {
	profane := "****"
	var words []string
	words = strings.Split(text, " ")
	for i, word := range words {
		switch strings.ToLower(word) {
		case "kerfuffle", "sharbert", "fornax":
			words[i] = profane
		}
	}

	return strings.Join(words, " ")
}

func respondWithError(w http.ResponseWriter, status int, msg string) {
	type errorJson struct {
		Error string `json:"error"`
	}
	resp := errorJson{
		Error: msg,
	}
	dat, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Error marshalling JSON: %s", err)
		w.WriteHeader(500)
		return
	}

	log.Printf("Error decoding parameters: %s", err)
	w.WriteHeader(status)
	w.Write(dat)
}

func responseWithJson(w http.ResponseWriter, status int, payload interface{}) {
	dat, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshalling JSON: %s", err)
		w.WriteHeader(500)
		return
	}
	w.WriteHeader(status)
	w.Write(dat)
}
