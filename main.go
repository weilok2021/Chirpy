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
	"github.com/weilok2021/Chirpy/internal/auth"
	"github.com/weilok2021/Chirpy/internal/database"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	db             *database.Queries
	platform       string
	secretKey      string
}

type Chirp struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	UserID    uuid.UUID `json:"user_id"`
}

type User struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
}

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	cfg := apiConfig{fileserverHits: atomic.Int32{}, platform: os.Getenv("PLATFORM"), secretKey: os.Getenv("JWT_SECRET_KEY")}

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
	mux.HandleFunc("GET /admin/metrics", (&cfg).handlerCountRequests)
	mux.HandleFunc("POST /admin/reset", (&cfg).handlerReset)
	mux.HandleFunc("POST /api/users", (&cfg).handlerCreateUser)
	mux.HandleFunc("POST /api/chirps", (&cfg).handlerCreateChirp)
	mux.HandleFunc("POST /api/login", cfg.handlerLogin)
	mux.HandleFunc("POST /api/refresh", cfg.handlerRefreshToken)
	mux.HandleFunc("POST /api/revoke", cfg.handlerRevokeToken)
	mux.HandleFunc("PUT /api/users", cfg.handlerUpdateUser)

	// get apis
	mux.HandleFunc("GET /api/chirps", cfg.handlerListChirps)
	mux.HandleFunc("GET /api/chirps/{chirpID}", cfg.handlerGetChirp)

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
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	req := requestJson{}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		responseWithError(w, 500, "Error occured while decoding request body", err)
		return
	}

	hashedPassword, err := auth.HashPassword(req.Password)
	if err != nil {
		responseWithError(w, 500, "Error occured while hashing new password", err)
		return
	}
	dbUser, err := cfg.db.CreateUser(r.Context(), database.CreateUserParams{
		Email:          req.Email,
		HashedPassword: hashedPassword,
	})

	// convert database.User to main.User(to have json field in response)
	// Purposely exclude HashedPassword in json response for security purpose
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

func (cfg *apiConfig) handlerUpdateUser(w http.ResponseWriter, r *http.Request) {
	type requestJson struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	req := requestJson{}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		responseWithError(w, 500, "Error occured while decoding login request", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		responseWithError(w, 401, "invalid token", err)
	}
	userID, err := auth.ValidateJWT(token, cfg.secretKey)
	if err != nil {
		responseWithError(w, 401, "invalid token", err)
	}
	hashedPassword, _ := auth.HashPassword(req.Password)
	if err := cfg.db.UpdateUser(r.Context(), database.UpdateUserParams{
		Email:          req.Email,
		HashedPassword: hashedPassword,
		ID:             userID,
	}); err != nil {
		responseWithError(w, 401, "User does not exist in db", err)
	}
	user, err := cfg.db.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		responseWithError(w, 500, "update email request failed.", err)
	}
	// response a user payload without password.
	responseWithJson(w, 200, User{
		ID:        user.ID,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		Email:     user.Email,
	})
}

func (cfg *apiConfig) handlerCreateChirp(w http.ResponseWriter, r *http.Request) {
	type requestJson struct {
		Body string `json:"body"`
	}

	tokenString, err := auth.GetBearerToken(r.Header)
	if err != nil {
		responseWithError(w, 401, "401 Unauthorized", err)
		return
	}

	// Decode request body after we get tokenString from client request header
	decoder := json.NewDecoder(r.Body)
	req := requestJson{}
	decodeErr := decoder.Decode(&req)
	if decodeErr != nil {
		responseWithError(w, 500, "Error occured while decoding request", err)
		return
	}

	// Get user's uuid with jwt token string
	userID, err := auth.ValidateJWT(tokenString, cfg.secretKey)
	if err != nil {
		responseWithError(w, 401, "401 Unauthorized", err)
	}

	if len(req.Body) > 140 {
		responseWithError(w, 400, "Chirp is too long", err)
		return
	}

	formatted_body := replaceProfane(req.Body)
	chirp, err := cfg.db.CreateChirp(r.Context(), database.CreateChirpParams{
		Body:   formatted_body,
		UserID: userID, // userID get from jwt token
	})
	if err != nil {
		responseWithError(w, 500, "Error occured while creating chirp from db", err)
		return
	}

	// convert chirp to Chirp with json key defined
	responseWithJson(w, 201, Chirp{
		ID:        chirp.ID,
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
		Body:      chirp.Body,
		UserID:    chirp.UserID,
	})
}

func (cfg *apiConfig) handlerListChirps(w http.ResponseWriter, r *http.Request) {
	chirps, err := cfg.db.ListChirps(r.Context())
	if err != nil {
		responseWithError(w, 500, "Error occured while retrieving chirps from db", err)
		return
	}

	// convert each Chirp in database.[]Chirp into main.Chirp with json key defined
	jsonChirps := make([]Chirp, len(chirps))
	for i, chirp := range chirps {
		jsonChirps[i] = Chirp{
			ID:        chirp.ID,
			CreatedAt: chirp.CreatedAt,
			UpdatedAt: chirp.UpdatedAt,
			Body:      chirp.Body,
			UserID:    chirp.UserID,
		}
	}
	responseWithJson(w, 200, jsonChirps)
}

func (cfg *apiConfig) handlerGetChirp(w http.ResponseWriter, r *http.Request) {
	chirpIDString := r.PathValue("chirpID")
	// Convert string to uuid.UUID
	chirpID, err := uuid.Parse(chirpIDString)
	if err != nil {
		responseWithError(w, 500, "failed to parse UUID", err)
		return
	}

	chirp, err := cfg.db.GetChirp(r.Context(), chirpID)
	if err != nil {
		responseWithError(w, 404, "Chirp not exist in db", err)
		return
	}
	responseWithJson(w, 200, Chirp{
		ID:        chirp.ID,
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
		Body:      chirp.Body,
		UserID:    chirp.UserID,
	})
}

// this handler method will response with 2 tokens:
//   - JWTToken/access token: stateless token that store user info, last for only 1 hour
//   - refresh_token: stateful token that store in our database, revoke is possible
//     to prevent attacker access our api, the meaning of refresh is client can get
//     new access token from us is they has valid refresh token in our database.
func (cfg *apiConfig) handlerLogin(w http.ResponseWriter, r *http.Request) {
	type requestJson struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	req := requestJson{}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		responseWithError(w, 500, "Error occured while decoding login request", err)
		return
	}

	user, err := cfg.db.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		responseWithError(w, 401, "Incorrect email", err)
		return
	}

	passwordMatched, err := auth.CheckPasswordHash(req.Password, user.HashedPassword)
	if err != nil {
		responseWithError(w, 401, "Error occured while checking password", err)
		return
	}
	if !passwordMatched {
		responseWithJson(w, 401, struct {
			Message string `json:"message"`
		}{
			Message: "Incorrect Password",
		})
		return
	}

	// jwt access token expired in 1 hour
	var jwtExpiresIn time.Duration = time.Hour

	// generate jwt token string
	jwtTokenString, err := auth.MakeJWT(user.ID, cfg.secretKey, jwtExpiresIn)
	if err != nil {
		return
	}

	// generate a refresh token string
	refreshTokenString := auth.MakeRefreshToken()
	// define DAY constant as 24 hours
	const DAY time.Duration = time.Hour * 24
	cfg.db.CreateRefreshToken(r.Context(), database.CreateRefreshTokenParams{
		Token:     refreshTokenString,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(60 * DAY),
	})

	// return a struct that embed User struct and with extra field: "token"
	responseWithJson(w, 200, struct {
		User                // Embedded struct to get all fields definition from User
		JwtToken     string `json:"token"`
		RefreshToken string `json:"refresh_token"`
	}{
		User: User{
			ID:        user.ID,
			CreatedAt: user.CreatedAt,
			UpdatedAt: user.UpdatedAt,
			Email:     user.Email,
		},
		JwtToken:     jwtTokenString,
		RefreshToken: refreshTokenString,
	})
}

func (cfg *apiConfig) handlerRefreshToken(w http.ResponseWriter, r *http.Request) {
	clientToken, err := auth.GetBearerToken(r.Header)
	if err != nil {
		responseWithError(w, 401, "Error occured while extracting bearer token", err)
		return
	}

	dbToken, err := cfg.db.GetRefreshToken(r.Context(), clientToken)
	if err != nil {
		responseWithError(w, 401, "Refresh token does not exist", err)
		return
	}

	// refresh token expired
	if time.Now().After(dbToken.ExpiresAt) {
		responseWithError(w, 401, "Refresh token Expired", err)
		return
	}

	// refresh token revoked
	// sql.Nulltime.Valid returns true when value is not NULL,
	// in our case, RevokedAt not null represents revoked before
	if dbToken.RevokedAt.Valid {
		responseWithError(w, 401, "Refresh token revoked by user", err)
		return
	}

	user, err := cfg.db.GetUserFromRefreshToken(r.Context(), dbToken.Token)
	if err != nil {
		responseWithError(w, 401, "Error occured while retrieving user by refresh token from db", err)
		return
	}

	// jwt access token expired in 1 hour
	var jwtExpiresIn time.Duration = time.Hour
	// generate new jwt access token for user
	jwtTokenString, err := auth.MakeJWT(user.ID, cfg.secretKey, jwtExpiresIn)
	if err != nil {
		responseWithError(w, 401, "JWT token creation error", err)
		return
	}

	responseWithJson(w, 200, struct {
		Token string `json:"token"`
	}{
		Token: jwtTokenString,
	})
}
func (cfg *apiConfig) handlerRevokeToken(w http.ResponseWriter, r *http.Request) {
	clientToken, err := auth.GetBearerToken(r.Header)
	if err != nil {
		responseWithError(w, 401, "Error occured while extracting bearer token", err)
		return
	}
	if _, err := cfg.db.UpdateRefreshToken(r.Context(), database.UpdateRefreshTokenParams{
		RevokedAt: sql.NullTime{Time: time.Now(), Valid: true},
		UpdatedAt: time.Now(),
		Token:     clientToken,
	}); err != nil {
		responseWithError(w, 401, "Can't revoke token that is invalid", err)
		return
	}

	responseWithJson(w, 204, struct{}{})
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

	// this method will returns json
	w.Header().Set("Content-Type", "application/json")
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
	// this method will returns json

	w.Header().Set("Content-Type", "application/json")
	dat, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshalling JSON: %s", err)
		w.WriteHeader(500)
		return
	}
	w.WriteHeader(status)
	w.Write(dat)
}
