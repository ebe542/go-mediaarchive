package api_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/api"
	appmedia "github.com/ebe542/go-mediaarchive/internal/application/media"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

type recordingMediaContentService struct {
	opened  appmedia.ReadableContent
	err     error
	actor   identity.User
	mediaID string
	calls   int
}

func (service *recordingMediaContentService) Open(
	_ context.Context,
	actor identity.User,
	mediaID string,
) (appmedia.ReadableContent, error) {
	service.calls++
	service.actor = actor
	service.mediaID = mediaID

	return service.opened, service.err
}

type apiContentReader struct {
	*bytes.Reader
	closed  bool
	seekErr error
}

func (reader *apiContentReader) Seek(offset int64, whence int) (int64, error) {
	if reader.seekErr != nil {
		return 0, reader.seekErr
	}

	return reader.Reader.Seek(offset, whence)
}

func (reader *apiContentReader) Close() error {
	reader.closed = true

	return nil
}

func TestMediaContentRequiresAuthentication(t *testing.T) {
	service := &recordingMediaContentService{}
	handler := api.NewHandler(api.WithMediaContentAPI(
		&recordingSessionResolver{},
		service,
	))
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/media/"+apiMediaID+"/content",
		nil,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", response.Code)
	}
	if service.calls != 0 {
		t.Fatal("expected unauthenticated request not to reach service")
	}
}

func TestMediaContentStreamsCompleteRepresentation(t *testing.T) {
	data := mediaContentData()
	service, reader := contentService(t, data)
	resolver := mediaResolver()
	handler := api.NewHandler(api.WithMediaContentAPI(resolver, service))
	request := authenticatedContentRequest(http.MethodGet)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	if !bytes.Equal(response.Body.Bytes(), data) {
		t.Fatalf("unexpected content body length %d", response.Body.Len())
	}
	if service.calls != 1 || service.mediaID != apiMediaID ||
		service.actor.ID != resolver.user.ID {
		t.Fatalf("unexpected service call: %+v", service)
	}
	assertContentHeaders(t, response, int64(len(data)), "")
	if !reader.closed {
		t.Fatal("expected streamed content to be closed")
	}
}

func TestMediaContentStreamsSingleRanges(t *testing.T) {
	data := mediaContentData()
	testCases := []struct {
		name         string
		header       string
		expected     []byte
		contentRange string
	}{
		{
			name:         "bounded",
			header:       "bytes=10-19",
			expected:     data[10:20],
			contentRange: "bytes 10-19/4096",
		},
		{
			name:         "open",
			header:       "bytes=4086-",
			expected:     data[4086:],
			contentRange: "bytes 4086-4095/4096",
		},
		{
			name:         "suffix",
			header:       "bytes=-10",
			expected:     data[4086:],
			contentRange: "bytes 4086-4095/4096",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			service, reader := contentService(t, data)
			handler := api.NewHandler(api.WithMediaContentAPI(mediaResolver(), service))
			request := authenticatedContentRequest(http.MethodGet)
			request.Header.Set("Range", testCase.header)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusPartialContent {
				t.Fatalf("expected status 206, got %d: %s", response.Code, response.Body.String())
			}
			if !bytes.Equal(response.Body.Bytes(), testCase.expected) {
				t.Fatalf("expected %v, got %v", testCase.expected, response.Body.Bytes())
			}
			assertContentHeaders(
				t,
				response,
				int64(len(testCase.expected)),
				testCase.contentRange,
			)
			if !reader.closed {
				t.Fatal("expected ranged content to be closed")
			}
		})
	}
}

func TestMediaContentHeadReturnsHeadersWithoutBody(t *testing.T) {
	data := mediaContentData()
	testCases := []struct {
		name         string
		rangeHeader  string
		status       int
		length       int64
		contentRange string
	}{
		{name: "complete", status: http.StatusOK, length: 4096},
		{
			name:         "partial",
			rangeHeader:  "bytes=10-19",
			status:       http.StatusPartialContent,
			length:       10,
			contentRange: "bytes 10-19/4096",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			service, reader := contentService(t, data)
			handler := api.NewHandler(api.WithMediaContentAPI(mediaResolver(), service))
			request := authenticatedContentRequest(http.MethodHead)
			if testCase.rangeHeader != "" {
				request.Header.Set("Range", testCase.rangeHeader)
			}
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != testCase.status {
				t.Fatalf("expected status %d, got %d", testCase.status, response.Code)
			}
			if response.Body.Len() != 0 {
				t.Fatalf("expected empty HEAD body, got %d bytes", response.Body.Len())
			}
			assertContentHeaders(t, response, testCase.length, testCase.contentRange)
			if !reader.closed {
				t.Fatal("expected HEAD content to be closed")
			}
		})
	}
}

func TestMediaContentRejectsInvalidRangesAsJSON(t *testing.T) {
	for _, rangeHeader := range []string{
		"items=0-1",
		"bytes=0-1,3-4",
		"bytes=4096-",
		"bytes=-0",
		"bytes=invalid",
	} {
		t.Run(rangeHeader, func(t *testing.T) {
			service, reader := contentService(t, mediaContentData())
			handler := api.NewHandler(api.WithMediaContentAPI(mediaResolver(), service))
			request := authenticatedContentRequest(http.MethodGet)
			request.Header.Set("Range", rangeHeader)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusRequestedRangeNotSatisfiable {
				t.Fatalf("expected status 416, got %d", response.Code)
			}
			if response.Header().Get("Content-Range") != "bytes */4096" ||
				response.Header().Get("Content-Type") != "application/json; charset=utf-8" ||
				!strings.Contains(response.Body.String(), `"code":"invalid_range"`) {
				t.Fatalf("unexpected range error response: %v %s", response.Header(), response.Body.String())
			}
			if !reader.closed {
				t.Fatal("expected rejected range content to be closed")
			}
		})
	}
}

func TestMediaContentHidesApplicationErrors(t *testing.T) {
	internalError := errors.New("private content path")
	for name, testCase := range map[string]struct {
		err    error
		status int
	}{
		"not found": {appmedia.ErrMediaNotFound, http.StatusNotFound},
		"internal":  {internalError, http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			service := &recordingMediaContentService{err: testCase.err}
			handler := api.NewHandler(api.WithMediaContentAPI(mediaResolver(), service))
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, authenticatedContentRequest(http.MethodGet))

			if response.Code != testCase.status {
				t.Fatalf("expected status %d, got %d", testCase.status, response.Code)
			}
			if strings.Contains(response.Body.String(), internalError.Error()) {
				t.Fatalf("expected internal details to be hidden, got %s", response.Body.String())
			}
		})
	}
}

func TestMediaContentHandlesSeekFailureBeforeBinaryResponse(t *testing.T) {
	service, reader := contentService(t, mediaContentData())
	reader.seekErr = errors.New("seek failure")
	handler := api.NewHandler(api.WithMediaContentAPI(mediaResolver(), service))
	request := authenticatedContentRequest(http.MethodGet)
	request.Header.Set("Range", "bytes=1-2")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError ||
		response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("unexpected seek failure response: %d %v", response.Code, response.Header())
	}
	if !reader.closed {
		t.Fatal("expected failed content to be closed")
	}
}

func contentService(
	test *testing.T,
	data []byte,
) (*recordingMediaContentService, *apiContentReader) {
	test.Helper()
	item := apiMediaItem(test, []string{"Example Author"})
	if int64(len(data)) != item.Size {
		test.Fatalf("expected fixture size %d, got %d", item.Size, len(data))
	}
	reader := &apiContentReader{Reader: bytes.NewReader(data)}

	return &recordingMediaContentService{
		opened: appmedia.ReadableContent{
			Item:         item,
			Reader:       reader,
			Size:         item.Size,
			LastModified: time.Date(2026, time.September, 20, 10, 0, 0, 0, time.UTC),
		},
	}, reader
}

func authenticatedContentRequest(method string) *http.Request {
	request := httptest.NewRequest(
		method,
		"/api/v1/media/"+apiMediaID+"/content",
		nil,
	)
	request.Header.Set("Authorization", "Bearer media-session")

	return request
}

func mediaContentData() []byte {
	data := make([]byte, 4096)
	for index := range data {
		data[index] = byte(index % 251)
	}

	return data
}

func assertContentHeaders(
	test *testing.T,
	response *httptest.ResponseRecorder,
	expectedLength int64,
	expectedRange string,
) {
	test.Helper()
	header := response.Header()
	if header.Get("Content-Type") != "application/pdf" ||
		header.Get("Content-Length") != strconv.FormatInt(expectedLength, 10) ||
		header.Get("Cache-Control") != "private, no-store" ||
		header.Get("X-Content-Type-Options") != "nosniff" ||
		header.Get("Accept-Ranges") != "bytes" ||
		header.Get("ETag") != `"`+strings.Repeat("5a", 32)+`"` ||
		header.Get("Last-Modified") != "Sun, 20 Sep 2026 10:00:00 GMT" ||
		header.Get("Content-Range") != expectedRange {
		test.Fatalf("unexpected content headers: %v", header)
	}
	disposition, parameters, err := mime.ParseMediaType(header.Get("Content-Disposition"))
	if err != nil || disposition != "inline" ||
		parameters["filename"] != "security-engineering.pdf" {
		test.Fatalf("unexpected content disposition %q", header.Get("Content-Disposition"))
	}
}

var _ io.ReadSeekCloser = (*apiContentReader)(nil)
