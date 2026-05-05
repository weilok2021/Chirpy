package auth

import (
	"errors"
	"net/http"
	"strings"
)

func GetAPIKey(headers http.Header) (string, error) {
	// Extract api key from this shape: "ApiKey THE_KEY_HERE"
	apiKeyHeader := headers.Get("Authorization")
	if apiKeyHeader == "" {
		return "", errors.New("API Key does not exist in request header!")
	}
	apiKeySlice := strings.Split(apiKeyHeader, " ")
	if len(apiKeySlice) != 2 {
		return "", errors.New("This is not a valid API key header")
	}
	return apiKeySlice[1], nil
}
