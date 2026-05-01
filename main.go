package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/weilok2021/Chirpy/internal/database"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	db             *database.Queries
	platform       string
}

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	cfg := apiConfig{fileserverHits: atomic.Int32{}, platform: os.Getenv("PLATFORM")}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal(err)
	}

	cfg.db = database.New(db)
	mux := http.NewServeMux()

	server := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	mux.Handle("/app/", (&cfg).middlewareMetricsInc(http.StripPrefix("/app/", http.FileServer(http.Dir(".")))))
	mux.Handle("/app/logo.png", (&cfg).middlewareMetricsInc(http.StripPrefix("/app/", http.FileServer(http.Dir("./assets")))))

	mux.HandleFunc("GET /api/healthz", handlerReadiness)
	mux.HandleFunc("POST /api/validate_chirp", handlerValidateChirp)
	mux.HandleFunc("GET /admin/metrics", (&cfg).handlerCountRequests)
	mux.HandleFunc("POST /admin/reset", (&cfg).handlerReset)
	mux.HandleFunc("POST /api/users", (&cfg).handlerCreateUser)

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
		responseWithError(w, 500, "Something went wrong", err)
		return
	}

	if len(req.Body) > 140 {
		responseWithError(w, 400, "Chirp is too long", err)
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

func (cfg *apiConfig) handlerReset(w http.ResponseWriter, r *http.Request) {
	if cfg.platform != "dev" {
		responseWithError(w, 403, "You've not configured the dev environment", errors.New("403 Forbidden"))
		return
	}
	cfg.fileserverHits.Store(0)
	if err := cfg.db.DeleteAllUsers(r.Context()); err != nil {
		responseWithError(w, 500, "Error deleting all users", err)
		return
	}
	responseWithJson(w, 200, struct {
		Status string `json:"status"`
	}{
		Status: "200--Success",
	})
}

func (cfg *apiConfig) handlerCreateUser(w http.ResponseWriter, r *http.Request) {
	type requestJson struct {
		Email string `json:"email"`
	}
	type User struct {
		ID        uuid.UUID `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		Email     string    `json:"email"`
	}
	req := requestJson{}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		responseWithError(w, 500, "Error occured while decoding request body", err)
		return
	}

	dbUser, err := cfg.db.CreateUser(r.Context(), req.Email)

	// map database.User to main.User(to have json field in response)
	jsonUser := User{
		ID:        dbUser.ID,
		CreatedAt: dbUser.CreatedAt,
		UpdatedAt: dbUser.UpdatedAt,
		Email:     dbUser.Email,
	}
	if err != nil {
		responseWithError(w, 500, "Error occured while creating new user.", err)
		return
	}

	responseWithJson(w, 201, jsonUser)
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

func responseWithError(w http.ResponseWriter, status int, msg string, rootCause error) {
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

	fmt.Printf("Show the exact error: %v\n\n\n\n", rootCause)
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
