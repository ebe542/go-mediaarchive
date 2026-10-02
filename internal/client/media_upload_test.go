package client_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/client"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

func TestUploadMediaStreamsMultipartRequest(t *testing.T) {
	data := []byte("streamed media content")
	checksum := sha256.Sum256(data)
	server := httptest.NewServer(http.HandlerFunc(func(
		argResponse http.ResponseWriter,
		argRequest *http.Request,
	) {
		if argRequest.Method != http.MethodPost || argRequest.URL.Path != "/api/v1/media/uploads" {
			t.Errorf("unexpected request %s %s", argRequest.Method, argRequest.URL.Path)
		}
		if argRequest.Header.Get("Authorization") != "Bearer upload-token" {
			t.Errorf("unexpected authorization header %q", argRequest.Header.Get("Authorization"))
		}
		mediaType, parameters, err := mime.ParseMediaType(argRequest.Header.Get("Content-Type"))
		if err != nil || mediaType != "multipart/form-data" {
			t.Fatalf("parse multipart Content-Type: %v", err)
		}
		reader := multipart.NewReader(argRequest.Body, parameters["boundary"])
		metadataPart, err := reader.NextPart()
		if err != nil {
			t.Fatalf("read metadata part: %v", err)
		}
		var metadata struct {
			Title   string           `json:"title"`
			Authors []string         `json:"authors"`
			Type    domainmedia.Type `json:"type"`
		}
		if err := json.NewDecoder(metadataPart).Decode(&metadata); err != nil {
			t.Fatalf("decode metadata: %v", err)
		}
		filePart, err := reader.NextPart()
		if err != nil {
			t.Fatalf("read file part: %v", err)
		}
		written, err := io.ReadAll(filePart)
		if err != nil {
			t.Fatalf("read file content: %v", err)
		}
		if metadata.Title != "Security Engineering" ||
			filePart.FileName() != "security.pdf" ||
			filePart.Header.Get("Content-Type") != "application/pdf" ||
			!bytes.Equal(written, data) {
			t.Fatalf("unexpected multipart upload: %+v %q %q", metadata, filePart.FileName(), written)
		}

		argResponse.Header().Set("Content-Type", "application/json")
		argResponse.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprintf(
			argResponse,
			`{"id":"123e4567-e89b-12d3-a456-426614174000",`+
				`"title":"Security Engineering","authors":["Example Author"],`+
				`"originalFilename":"security.pdf","type":"book",`+
				`"mimeType":"application/pdf","size":%d,"sha256":"%x",`+
				`"ownerId":"223e4567-e89b-12d3-a456-426614174000",`+
				`"createdAt":"2026-09-20T10:00:00Z",`+
				`"updatedAt":"2026-09-20T10:00:00Z"}`,
			len(data),
			checksum,
		)
	}))
	t.Cleanup(server.Close)

	apiClient := client.New(server.URL, &http.Client{Timeout: 5 * time.Second})
	mediaItem, err := apiClient.UploadMedia(
		context.Background(),
		"upload-token",
		client.MediaUploadInput{
			Title:            "Security Engineering",
			Authors:          []string{"Example Author"},
			OriginalFilename: "security.pdf",
			Type:             domainmedia.TypeBook,
			MIMEType:         "application/pdf",
			Source:           bytes.NewReader(data),
		},
	)
	if err != nil {
		t.Fatalf("upload media: %v", err)
	}
	if mediaItem.Size != int64(len(data)) || mediaItem.Checksum != checksum {
		t.Fatalf("unexpected uploaded media: %+v", mediaItem)
	}
}

func TestUploadMediaRejectsMissingSource(t *testing.T) {
	apiClient := client.New("http://127.0.0.1:1", nil)
	_, err := apiClient.UploadMedia(
		context.Background(),
		"token",
		client.MediaUploadInput{},
	)
	if err == nil {
		t.Fatal("expected missing upload source error")
	}
}
