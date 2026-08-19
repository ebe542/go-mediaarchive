// Package session defines opaque server-side authentication sessions.
package session

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

const tokenRandomLength int = 32

// TokenGenerator creates opaque session tokens and their storage hashes.
type TokenGenerator struct {
	random io.Reader
}

// NewTokenGenerator creates a token generator with an explicit random source.
func NewTokenGenerator(argRandom io.Reader) *TokenGenerator {
	return &TokenGenerator{
		random: argRandom,
	}
}

// NewDefaultTokenGenerator creates a production token generator.
func NewDefaultTokenGenerator() *TokenGenerator {
	return NewTokenGenerator(rand.Reader)
}

// Generate creates a 256-bit opaque token and its SHA-256 storage hash.
func (generator *TokenGenerator) Generate() (
	string,
	[sha256.Size]byte,
	error,
) {
	randomBytes := make([]byte, tokenRandomLength)

	if _, err := io.ReadFull(
		generator.random,
		randomBytes,
	); err != nil {
		return "", [sha256.Size]byte{}, fmt.Errorf(
			"read session token randomness: %w",
			err,
		)
	}

	token := base64.RawURLEncoding.EncodeToString(randomBytes)
	tokenHash := sha256.Sum256([]byte(token))

	return token, tokenHash, nil
}

// HashToken creates the storage hash for a presented opaque token.
func HashToken(argToken string) [sha256.Size]byte {
	return sha256.Sum256([]byte(argToken))
}
