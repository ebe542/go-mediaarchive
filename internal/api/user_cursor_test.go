package api

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

	appusers "github.com/ebe542/go-mediaarchive/internal/application/users"
)

func TestUserCursorCodecRoundTripsCanonicalValues(t *testing.T) {
	expected, err := appusers.NewCursor(
		time.Date(2026, time.August, 29, 10, 0, 0, 123, time.UTC),
		"123e4567-e89b-12d3-a456-426614174000",
	)
	if err != nil {
		t.Fatalf("create cursor: %v", err)
	}

	encoded, err := encodeUserCursor(expected)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	if encoded == "" {
		t.Fatal("expected non-empty cursor")
	}
	if encoded[len(encoded)-1] == '=' {
		t.Fatal("expected unpadded Base64URL cursor")
	}

	actual, err := decodeUserCursor(encoded)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	if actual != expected {
		t.Fatalf("expected cursor %+v, got %+v", expected, actual)
	}
}

func TestUserCursorDecoderRejectsMalformedDocuments(t *testing.T) {
	encodeDocument := func(argDocument string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(argDocument))
	}

	testCases := map[string]string{
		"empty":             "",
		"invalid Base64URL": "***",
		"missing fields":    encodeDocument(`{}`),
		"unknown field": encodeDocument(
			`{"createdAt":"2026-08-29T10:00:00Z","id":"123e4567-e89b-12d3-a456-426614174000","unexpected":true}`,
		),
		"duplicate field": encodeDocument(
			`{"createdAt":"2026-08-29T10:00:00Z","createdAt":"2026-08-29T11:00:00Z","id":"123e4567-e89b-12d3-a456-426614174000"}`,
		),
		"non-canonical timestamp": encodeDocument(
			`{"createdAt":"2026-08-29T12:00:00+02:00","id":"123e4567-e89b-12d3-a456-426614174000"}`,
		),
		"invalid user ID": encodeDocument(
			`{"createdAt":"2026-08-29T10:00:00Z","id":"not-a-uuid"}`,
		),
		"trailing document": encodeDocument(
			`{"createdAt":"2026-08-29T10:00:00Z","id":"123e4567-e89b-12d3-a456-426614174000"}{}`,
		),
	}

	for name, encoded := range testCases {
		t.Run(name, func(t *testing.T) {
			_, err := decodeUserCursor(encoded)
			if !errors.Is(err, appusers.ErrInvalidCursor) {
				t.Fatalf("expected ErrInvalidCursor, got %v", err)
			}
		})
	}
}
