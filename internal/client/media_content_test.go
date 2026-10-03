package client_test

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

	"github.com/ebe542/go-mediaarchive/internal/client"
)

const contentMediaID = "123e4567-e89b-12d3-a456-426614174000"

func TestStreamMediaContentWritesCompleteResponse(t *testing.T) {
	data := []byte("complete streamed content")
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		if request.Method != http.MethodGet ||
			request.URL.Path != "/api/v1/media/"+contentMediaID+"/content" {
			t.Errorf("unexpected request %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer content-token" {
			t.Errorf("unexpected authorization header %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("Range") != "" {
			t.Errorf("unexpected Range header %q", request.Header.Get("Range"))
		}
		writeClientContentResponse(response, http.StatusOK, data, "")
	}))
	t.Cleanup(server.Close)

	apiClient := client.New(server.URL, server.Client())
	var destination bytes.Buffer
	metadata, err := apiClient.StreamMediaContent(
		context.Background(),
		"content-token",
		contentMediaID,
		&destination,
		client.CompleteContent(),
	)
	if err != nil {
		t.Fatalf("stream complete content: %v", err)
	}
	if !bytes.Equal(destination.Bytes(), data) {
		t.Fatalf("expected %q, got %q", data, destination.Bytes())
	}
	assertClientContentMetadata(t, metadata, int64(len(data)), nil)
}

func TestStreamMediaContentWritesValidatedRanges(t *testing.T) {
	data := []byte("0123456789")
	testCases := []struct {
		name         string
		createRange  func() (client.ByteRange, error)
		header       string
		body         []byte
		contentRange string
		expected     client.ReturnedByteRange
	}{
		{
			name: "bounded",
			createRange: func() (client.ByteRange, error) {
				return client.BoundedRange(2, 5)
			},
			header:       "bytes=2-5",
			body:         data[2:6],
			contentRange: "bytes 2-5/10",
			expected:     client.ReturnedByteRange{Start: 2, End: 5, Total: 10},
		},
		{
			name: "open ended",
			createRange: func() (client.ByteRange, error) {
				return client.OpenEndedRange(6)
			},
			header:       "bytes=6-",
			body:         data[6:],
			contentRange: "bytes 6-9/10",
			expected:     client.ReturnedByteRange{Start: 6, End: 9, Total: 10},
		},
		{
			name: "suffix",
			createRange: func() (client.ByteRange, error) {
				return client.SuffixRange(3)
			},
			header:       "bytes=-3",
			body:         data[7:],
			contentRange: "bytes 7-9/10",
			expected:     client.ReturnedByteRange{Start: 7, End: 9, Total: 10},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(
				response http.ResponseWriter,
				request *http.Request,
			) {
				if request.Header.Get("Range") != testCase.header {
					t.Errorf("expected Range %q, got %q", testCase.header, request.Header.Get("Range"))
				}
				writeClientContentResponse(
					response,
					http.StatusPartialContent,
					testCase.body,
					testCase.contentRange,
				)
			}))
			t.Cleanup(server.Close)
			requestedRange, err := testCase.createRange()
			if err != nil {
				t.Fatalf("create byte range: %v", err)
			}
			var destination bytes.Buffer

			metadata, err := client.New(server.URL, server.Client()).StreamMediaContent(
				context.Background(),
				"token",
				contentMediaID,
				&destination,
				requestedRange,
			)
			if err != nil {
				t.Fatalf("stream ranged content: %v", err)
			}
			if !bytes.Equal(destination.Bytes(), testCase.body) {
				t.Fatalf("expected %q, got %q", testCase.body, destination.Bytes())
			}
			assertClientContentMetadata(
				t,
				metadata,
				int64(len(testCase.body)),
				&testCase.expected,
			)
		})
	}
}

func TestContentRangeConstructorsRejectInvalidValues(t *testing.T) {
	for name, createRange := range map[string]func() error{
		"negative bounded start": func() error {
			_, err := client.BoundedRange(-1, 2)
			return err
		},
		"reversed bounded range": func() error {
			_, err := client.BoundedRange(2, 1)
			return err
		},
		"negative open start": func() error {
			_, err := client.OpenEndedRange(-1)
			return err
		},
		"zero suffix": func() error {
			_, err := client.SuffixRange(0)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := createRange(); err == nil {
				t.Fatal("expected invalid range error")
			}
		})
	}
}

func TestStreamMediaContentRejectsMissingDestinationWithoutRequest(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requestCount++
	}))
	t.Cleanup(server.Close)

	_, err := client.New(server.URL, server.Client()).StreamMediaContent(
		context.Background(),
		"token",
		contentMediaID,
		nil,
		client.CompleteContent(),
	)
	if err == nil {
		t.Fatal("expected missing destination error")
	}
	if requestCount != 0 {
		t.Fatalf("expected no request, got %d", requestCount)
	}
}

func TestStreamMediaContentReturnsBoundedAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(
			response,
			`{"error":{"code":"not_found","message":"Resource not found."}}`,
		)
	}))
	t.Cleanup(server.Close)
	var destination bytes.Buffer

	_, err := client.New(server.URL, server.Client()).StreamMediaContent(
		context.Background(),
		"token",
		contentMediaID,
		&destination,
		client.CompleteContent(),
	)
	var apiError *client.APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 APIError, got %v", err)
	}
	if destination.Len() != 0 {
		t.Fatalf("expected untouched destination, got %q", destination.Bytes())
	}
}

func TestStreamMediaContentValidatesHeadersBeforeWriting(t *testing.T) {
	testCases := []struct {
		name   string
		header string
		value  string
	}{
		{"content type", "Content-Type", "application/pdf; charset=utf-8"},
		{"disposition", "Content-Disposition", "attachment; filename=content.pdf"},
		{"length", "Content-Length", "invalid"},
		{"cache", "Cache-Control", "public"},
		{"nosniff", "X-Content-Type-Options", ""},
		{"ranges", "Accept-Ranges", "none"},
		{"etag", "ETag", `W/"` + strings.Repeat("5a", 32) + `"`},
		{"modified", "Last-Modified", "invalid"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(
				response http.ResponseWriter,
				_ *http.Request,
			) {
				setClientContentHeaders(response.Header(), 7, "")
				if testCase.value == "" {
					response.Header().Del(testCase.header)
				} else {
					response.Header().Set(testCase.header, testCase.value)
				}
				response.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(response, "content")
			}))
			t.Cleanup(server.Close)
			var destination bytes.Buffer

			_, err := client.New(server.URL, server.Client()).StreamMediaContent(
				context.Background(),
				"token",
				contentMediaID,
				&destination,
				client.CompleteContent(),
			)
			if err == nil {
				t.Fatal("expected response validation error")
			}
			if destination.Len() != 0 {
				t.Fatalf("expected untouched destination, got %q", destination.Bytes())
			}
		})
	}
}

func TestStreamMediaContentReportsShortBodyAfterPartialWrite(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		setClientContentHeaders(response.Header(), 10, "")
		response.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(response, "short")
	}))
	t.Cleanup(server.Close)
	var destination bytes.Buffer

	_, err := client.New(server.URL, server.Client()).StreamMediaContent(
		context.Background(),
		"token",
		contentMediaID,
		&destination,
		client.CompleteContent(),
	)
	if err == nil {
		t.Fatal("expected short response body error")
	}
	if destination.String() != "short" {
		t.Fatalf("expected partial output, got %q", destination.String())
	}
}

func TestStreamMediaContentReportsDestinationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		writeClientContentResponse(response, http.StatusOK, []byte("content"), "")
	}))
	t.Cleanup(server.Close)
	destinationError := errors.New("destination failure")
	destination := &failingContentWriter{err: destinationError}

	_, err := client.New(server.URL, server.Client()).StreamMediaContent(
		context.Background(),
		"token",
		contentMediaID,
		destination,
		client.CompleteContent(),
	)
	if !errors.Is(err, destinationError) {
		t.Fatalf("expected destination failure, got %v", err)
	}
}

type failingContentWriter struct {
	err error
}

func (writer *failingContentWriter) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, writer.err
	}

	return 1, writer.err
}

func writeClientContentResponse(
	response http.ResponseWriter,
	status int,
	body []byte,
	contentRange string,
) {
	setClientContentHeaders(response.Header(), int64(len(body)), contentRange)
	response.WriteHeader(status)
	_, _ = response.Write(body)
}

func setClientContentHeaders(
	header http.Header,
	length int64,
	contentRange string,
) {
	header.Set("Content-Type", "application/pdf")
	header.Set(
		"Content-Disposition",
		mime.FormatMediaType("inline", map[string]string{"filename": "content.pdf"}),
	)
	header.Set("Content-Length", strconv.FormatInt(length, 10))
	header.Set("Cache-Control", "private, no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Accept-Ranges", "bytes")
	header.Set("ETag", `"`+strings.Repeat("5a", 32)+`"`)
	header.Set("Last-Modified", "Sun, 20 Sep 2026 10:00:00 GMT")
	if contentRange != "" {
		header.Set("Content-Range", contentRange)
	}
}

func assertClientContentMetadata(
	test *testing.T,
	metadata client.MediaContentMetadata,
	length int64,
	expectedRange *client.ReturnedByteRange,
) {
	test.Helper()
	if metadata.MIMEType != "application/pdf" ||
		metadata.Filename != "content.pdf" ||
		metadata.Length != length ||
		metadata.ETag != `"`+strings.Repeat("5a", 32)+`"` ||
		!metadata.LastModified.Equal(
			time.Date(2026, time.September, 20, 10, 0, 0, 0, time.UTC),
		) {
		test.Fatalf("unexpected content metadata: %+v", metadata)
	}
	if expectedRange == nil {
		if metadata.Range != nil {
			test.Fatalf("expected no returned range, got %+v", metadata.Range)
		}

		return
	}
	if metadata.Range == nil || *metadata.Range != *expectedRange {
		test.Fatalf("expected range %+v, got %+v", *expectedRange, metadata.Range)
	}
}

var _ io.Writer = (*failingContentWriter)(nil)
