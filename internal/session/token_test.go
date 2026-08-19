package session

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestTokenGeneratorCreatesOpaqueTokenAndHash(t *testing.T) {
	randomBytes := bytes.Repeat([]byte{0x42}, 32)
	generator := NewTokenGenerator(bytes.NewReader(randomBytes))

	token, tokenHash, err := generator.Generate()
	if err != nil {
		t.Fatalf("generate session token: %v", err)
	}

	decodedToken, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("decode generated token: %v", err)
	}

	if !bytes.Equal(decodedToken, randomBytes) {
		t.Fatal("expected token to contain the supplied random bytes")
	}

	expectedHash := sha256.Sum256([]byte(token))
	if tokenHash != expectedHash {
		t.Fatalf(
			"expected token hash %x, got %x",
			expectedHash,
			tokenHash,
		)
	}

	if bytes.Contains([]byte(token), []byte("=")) {
		t.Fatal("expected unpadded URL-safe token")
	}
}
