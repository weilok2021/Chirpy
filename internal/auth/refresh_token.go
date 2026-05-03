package auth

import (
	"crypto/rand"
	"encoding/hex"
)

func MakeRefreshToken() string {
	// Note that no error handling is necessary, as Read always succeeds.
	randomBytes := make([]byte, 32)

	rand.Read(randomBytes)
	// convert random data to hexstring
	return hex.EncodeToString(randomBytes)
}
