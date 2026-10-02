package api_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"

	"github.com/ebe542/go-mediaarchive/internal/api"
	appmedia "github.com/ebe542/go-mediaarchive/internal/application/media"
	"github.com/ebe542/go-mediaarchive/internal/content"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

type recordingMediaUploadService struct {
	item    domainmedia.Item
	err     error
	actor   identity.User
	input   appmedia.UploadInput
	content []byte
	calls   int
}

func (service *recordingMediaUploadService) UploadItem(
	_ context.Context,
	actor identity.User,
	input appmedia.UploadInput,
) (domainmedia.Item, error) {
	service.calls++
	service.actor = actor
	service.input = input
	var readErr error
	service.content, readErr = io.ReadAll(input.Source)
	if readErr != nil {
		return domainmedia.Item{}, fmt.Errorf("%w: %v", content.ErrInvalidSource, readErr)
	}

	return service.item, service.err
}

func TestMediaUploadRequiresAuthentication(t *testing.T) {
	body, contentType := validMultipartUpload(t, false)
	service := &recordingMediaUploadService{}
	handler := api.NewHandler(api.WithMediaUploadAPI(
		&recordingSessionResolver{},
		service,
		1024,
	))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/media/uploads", body)
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", response.Code)
	}
	if service.calls != 0 {
		t.Fatal("expected unauthenticated upload not to reach service")
	}
}

func TestMediaUploadStreamsMultipartContent(t *testing.T) {
	body, contentType := validMultipartUpload(t, false)
	item := apiMediaItem(t, []string{"Example Author"})
	service := &recordingMediaUploadService{item: item}
	resolver := mediaResolver()
	handler := api.NewHandler(api.WithMediaUploadAPI(resolver, service, 1024))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/media/uploads", body)
	request.Header.Set("Authorization", "Bearer media-session")
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", response.Code, response.Body.String())
	}
	if service.calls != 1 || service.actor.ID != resolver.user.ID {
		t.Fatalf("expected authenticated actor, got %+v", service)
	}
	if service.input.Title != "Security Engineering" ||
		service.input.OriginalFilename != "security.pdf" ||
		service.input.MIMEType != "application/pdf" ||
		string(service.content) != "PDF content" {
		t.Fatalf("unexpected upload input: %+v %q", service.input, service.content)
	}
	if response.Header().Get("Location") != "/api/v1/media/"+item.ID ||
		response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected upload response headers: %v", response.Header())
	}
}

func TestMediaUploadRejectsUnexpectedPartBeforePersistence(t *testing.T) {
	body, contentType := validMultipartUpload(t, true)
	service := &recordingMediaUploadService{item: apiMediaItem(t, nil)}
	handler := api.NewHandler(api.WithMediaUploadAPI(mediaResolver(), service, 1024))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/media/uploads", body)
	request.Header.Set("Authorization", "Bearer media-session")
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", response.Code, response.Body.String())
	}
}

func TestMediaUploadMapsContentErrors(t *testing.T) {
	for name, testCase := range map[string]struct {
		err    error
		status int
	}{
		"empty":     {content.ErrEmpty, http.StatusBadRequest},
		"malformed": {content.ErrInvalidSource, http.StatusBadRequest},
		"oversized": {content.ErrTooLarge, http.StatusRequestEntityTooLarge},
	} {
		t.Run(name, func(t *testing.T) {
			body, contentType := validMultipartUpload(t, false)
			service := &recordingMediaUploadService{err: testCase.err}
			handler := api.NewHandler(api.WithMediaUploadAPI(mediaResolver(), service, 1024))
			request := httptest.NewRequest(http.MethodPost, "/api/v1/media/uploads", body)
			request.Header.Set("Authorization", "Bearer media-session")
			request.Header.Set("Content-Type", contentType)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != testCase.status {
				t.Fatalf("expected status %d, got %d", testCase.status, response.Code)
			}
			if errors.Is(testCase.err, content.ErrTooLarge) &&
				!bytes.Contains(response.Body.Bytes(), []byte(`"code":"content_too_large"`)) {
				t.Fatalf("unexpected oversized response %s", response.Body.String())
			}
		})
	}
}

func TestMediaUploadHidesOperationalErrors(t *testing.T) {
	body, contentType := validMultipartUpload(t, false)
	internalError := errors.New("private filesystem path")
	service := &recordingMediaUploadService{err: internalError}
	handler := api.NewHandler(api.WithMediaUploadAPI(mediaResolver(), service, 1024))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/media/uploads", body)
	request.Header.Set("Authorization", "Bearer media-session")
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", response.Code)
	}
	if bytes.Contains(response.Body.Bytes(), []byte(internalError.Error())) {
		t.Fatalf("expected internal details to be hidden, got %s", response.Body.String())
	}
}

func validMultipartUpload(
	test *testing.T,
	additionalPart bool,
) (*bytes.Buffer, string) {
	test.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	metadataHeader := make(textproto.MIMEHeader)
	metadataHeader.Set("Content-Disposition", `form-data; name="metadata"`)
	metadataHeader.Set("Content-Type", "application/json")
	metadata, err := writer.CreatePart(metadataHeader)
	if err != nil {
		test.Fatalf("create metadata part: %v", err)
	}
	if _, err := io.WriteString(
		metadata,
		`{"title":"Security Engineering","authors":["Example Author"],"type":"book"}`,
	); err != nil {
		test.Fatalf("write metadata: %v", err)
	}
	fileHeader := make(textproto.MIMEHeader)
	fileHeader.Set("Content-Disposition", `form-data; name="file"; filename="security.pdf"`)
	fileHeader.Set("Content-Type", "application/pdf")
	file, err := writer.CreatePart(fileHeader)
	if err != nil {
		test.Fatalf("create file part: %v", err)
	}
	if _, err := io.WriteString(file, "PDF content"); err != nil {
		test.Fatalf("write file: %v", err)
	}
	if additionalPart {
		extra, err := writer.CreateFormField("extra")
		if err != nil {
			test.Fatalf("create additional part: %v", err)
		}
		if _, err := io.WriteString(extra, "unexpected"); err != nil {
			test.Fatalf("write additional part: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		test.Fatalf("close multipart writer: %v", err)
	}

	return body, writer.FormDataContentType()
}
