package auth

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func MakeJWT(userID uuid.UUID, tokenSecret string, expiresIn time.Duration) (string, error) {
	claim := jwt.RegisteredClaims{
		Issuer:    "chirpy-access",
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiresIn)),
		Subject:   userID.String(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claim)
	signedJWT, err := token.SignedString([]byte(tokenSecret))
	if err != nil {
		return "", err
	}
	return signedJWT, nil
}

func ValidateJWT(tokenString, tokenSecret string) (uuid.UUID, error) {
	claim := &jwt.RegisteredClaims{}
	// An error will be returned if the token is invalid or has expired
	token, err := jwt.ParseWithClaims(tokenString, claim, func(t *jwt.Token) (any, error) {
		return []byte(tokenSecret), nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	if !token.Valid {
		return uuid.Nil, errors.New("invalid token")
	}

	sub, err := token.Claims.GetSubject()
	if err != nil {
		return uuid.Nil, err
	}
	userID, err := uuid.Parse(sub)
	if err != nil {
		return uuid.Nil, err
	}
	return userID, nil
}

func GetBearerToken(headers http.Header) (string, error) {
	// bearerToken format: "Bearer TOKEN_STRING"
	bearerToken := headers.Get("Authorization")
	if bearerToken == "" {
		return "", errors.New("Bearer Token does not exist in request header!")
	}
	// split bearer and tokenString
	splittedBearerToken := strings.Split(bearerToken, " ")
	if len(splittedBearerToken) != 2 {
		return "", errors.New("This is not a valid bearer token")
	}
	// return tokenString
	return splittedBearerToken[1], nil
}
