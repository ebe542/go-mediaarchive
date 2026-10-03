package client

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type byteRangeKind uint8

const (
	completeContent byteRangeKind = iota
	boundedContent
	openEndedContent
	suffixContent
)

// ByteRange describes an optional, validated single HTTP byte range.
type ByteRange struct {
	kind  byteRangeKind
	start int64
	end   int64
}

// CompleteContent requests the complete representation without a Range header.
func CompleteContent() ByteRange {
	return ByteRange{kind: completeContent}
}

// BoundedRange requests the inclusive byte interval from start through end.
func BoundedRange(start int64, end int64) (ByteRange, error) {
	if start < 0 || end < start {
		return ByteRange{}, errors.New("invalid bounded content range")
	}

	return ByteRange{kind: boundedContent, start: start, end: end}, nil
}

// OpenEndedRange requests all available bytes from start through the end.
func OpenEndedRange(start int64) (ByteRange, error) {
	if start < 0 {
		return ByteRange{}, errors.New("invalid open-ended content range")
	}

	return ByteRange{kind: openEndedContent, start: start}, nil
}

// SuffixRange requests the final length bytes of the representation.
func SuffixRange(length int64) (ByteRange, error) {
	if length <= 0 {
		return ByteRange{}, errors.New("invalid suffix content range")
	}

	return ByteRange{kind: suffixContent, end: length}, nil
}

// ReturnedByteRange describes the interval selected by a partial response.
type ReturnedByteRange struct {
	Start int64
	End   int64
	Total int64
}

// MediaContentMetadata describes a validated streamed content response.
type MediaContentMetadata struct {
	MIMEType     string
	Filename     string
	Length       int64
	ETag         string
	LastModified time.Time
	Range        *ReturnedByteRange
}

// StreamMediaContent writes one complete or partial media representation into
// destination. A transfer error may leave destination partially written.
func (client *Client) StreamMediaContent(
	ctx context.Context,
	accessToken string,
	mediaID string,
	destination io.Writer,
	requestedRange ByteRange,
) (MediaContentMetadata, error) {
	if destination == nil {
		return MediaContentMetadata{}, errors.New("media content destination is required")
	}
	rangeHeader, partial, err := requestedRange.headerValue()
	if err != nil {
		return MediaContentMetadata{}, err
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		client.baseURL+"/api/v1/media/"+url.PathEscape(mediaID)+"/content",
		nil,
	)
	if err != nil {
		return MediaContentMetadata{}, fmt.Errorf("create stream media content request: %w", err)
	}
	request.Header.Set("Accept", "*/*")
	if accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	if partial {
		request.Header.Set("Range", rangeHeader)
	}

	response, err := client.httpClient.Do(request)
	if err != nil {
		return MediaContentMetadata{}, fmt.Errorf("request stream media content: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	expectedStatus := http.StatusOK
	if partial {
		expectedStatus = http.StatusPartialContent
	}
	if response.StatusCode != expectedStatus {
		if response.StatusCode != http.StatusOK &&
			response.StatusCode != http.StatusPartialContent {
			responseErr := handleJSONResponse(
				response,
				expectedStatus,
				nil,
				"stream media content",
			)
			if responseErr != nil {
				return MediaContentMetadata{}, responseErr
			}
		}

		return MediaContentMetadata{}, fmt.Errorf(
			"request stream media content: unexpected HTTP status %s",
			response.Status,
		)
	}

	metadata, err := validateMediaContentResponse(response, requestedRange)
	if err != nil {
		return MediaContentMetadata{}, err
	}
	if _, err := io.CopyN(destination, response.Body, metadata.Length); err != nil {
		return MediaContentMetadata{}, fmt.Errorf("stream media content response: %w", err)
	}

	return metadata, nil
}

func (requested ByteRange) headerValue() (string, bool, error) {
	switch requested.kind {
	case completeContent:
		return "", false, nil
	case boundedContent:
		if requested.start < 0 || requested.end < requested.start {
			return "", false, errors.New("invalid bounded content range")
		}

		return fmt.Sprintf("bytes=%d-%d", requested.start, requested.end), true, nil
	case openEndedContent:
		if requested.start < 0 {
			return "", false, errors.New("invalid open-ended content range")
		}

		return fmt.Sprintf("bytes=%d-", requested.start), true, nil
	case suffixContent:
		if requested.end <= 0 {
			return "", false, errors.New("invalid suffix content range")
		}

		return fmt.Sprintf("bytes=-%d", requested.end), true, nil
	default:
		return "", false, errors.New("invalid content range kind")
	}
}

func validateMediaContentResponse(
	response *http.Response,
	requestedRange ByteRange,
) (MediaContentMetadata, error) {
	const operation = "stream media content"
	mediaType, parameters, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType == "" || len(parameters) != 0 {
		return MediaContentMetadata{}, fmt.Errorf("validate %s Content-Type", operation)
	}

	disposition, dispositionParameters, err := mime.ParseMediaType(
		response.Header.Get("Content-Disposition"),
	)
	filename := dispositionParameters["filename"]
	if err != nil || disposition != "inline" || !safeContentFilename(filename) {
		return MediaContentMetadata{}, fmt.Errorf("validate %s Content-Disposition", operation)
	}

	length, err := strconv.ParseInt(response.Header.Get("Content-Length"), 10, 64)
	if err != nil || length <= 0 || response.ContentLength != length {
		return MediaContentMetadata{}, fmt.Errorf("validate %s Content-Length", operation)
	}
	if !hasCacheDirective(response.Header.Values("Cache-Control"), "private") ||
		!hasCacheDirective(response.Header.Values("Cache-Control"), "no-store") {
		return MediaContentMetadata{}, fmt.Errorf("validate %s Cache-Control", operation)
	}
	if response.Header.Get("X-Content-Type-Options") != "nosniff" {
		return MediaContentMetadata{}, fmt.Errorf("validate %s X-Content-Type-Options", operation)
	}
	if response.Header.Get("Accept-Ranges") != "bytes" {
		return MediaContentMetadata{}, fmt.Errorf("validate %s Accept-Ranges", operation)
	}

	etag := response.Header.Get("ETag")
	if !validStrongSHA256ETag(etag) {
		return MediaContentMetadata{}, fmt.Errorf("validate %s ETag", operation)
	}
	lastModified, err := time.Parse(http.TimeFormat, response.Header.Get("Last-Modified"))
	if err != nil {
		return MediaContentMetadata{}, fmt.Errorf("validate %s Last-Modified: %w", operation, err)
	}

	metadata := MediaContentMetadata{
		MIMEType:     mediaType,
		Filename:     filename,
		Length:       length,
		ETag:         etag,
		LastModified: lastModified,
	}
	if response.StatusCode == http.StatusOK {
		if requestedRange.kind != completeContent || response.Header.Get("Content-Range") != "" {
			return MediaContentMetadata{}, fmt.Errorf("validate %s complete response", operation)
		}

		return metadata, nil
	}

	returnedRange, err := parseReturnedContentRange(
		response.Header.Get("Content-Range"),
		length,
	)
	if err != nil || !requestedRange.matches(returnedRange) {
		return MediaContentMetadata{}, fmt.Errorf("validate %s Content-Range", operation)
	}
	metadata.Range = &returnedRange

	return metadata, nil
}

func parseReturnedContentRange(value string, length int64) (ReturnedByteRange, error) {
	unit, value, found := strings.Cut(value, " ")
	interval, totalValue, totalFound := strings.Cut(value, "/")
	startValue, endValue, intervalFound := strings.Cut(interval, "-")
	if !found || unit != "bytes" || !totalFound || !intervalFound {
		return ReturnedByteRange{}, errors.New("invalid returned content range")
	}
	start, startErr := parseNonNegativeInt64(startValue)
	end, endErr := parseNonNegativeInt64(endValue)
	total, totalErr := parseNonNegativeInt64(totalValue)
	if startErr != nil || endErr != nil || totalErr != nil ||
		end < start || total <= end || end-start+1 != length {
		return ReturnedByteRange{}, errors.New("invalid returned content range")
	}

	return ReturnedByteRange{Start: start, End: end, Total: total}, nil
}

func parseNonNegativeInt64(value string) (int64, error) {
	if value == "" {
		return 0, errors.New("missing integer")
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, errors.New("invalid integer")
		}
	}

	return strconv.ParseInt(value, 10, 64)
}

func (requested ByteRange) matches(returned ReturnedByteRange) bool {
	switch requested.kind {
	case boundedContent:
		expectedEnd := requested.end
		if expectedEnd >= returned.Total {
			expectedEnd = returned.Total - 1
		}

		return returned.Start == requested.start &&
			returned.End == expectedEnd
	case openEndedContent:
		return returned.Start == requested.start &&
			returned.End == returned.Total-1
	case suffixContent:
		expectedLength := requested.end
		if expectedLength > returned.Total {
			expectedLength = returned.Total
		}

		return returned.End == returned.Total-1 &&
			returned.End-returned.Start+1 == expectedLength
	default:
		return false
	}
}

func validStrongSHA256ETag(value string) bool {
	if len(value) != hex.EncodedLen(32)+2 || value[0] != '"' || value[len(value)-1] != '"' {
		return false
	}
	encoded := value[1 : len(value)-1]
	if encoded != strings.ToLower(encoded) {
		return false
	}
	_, err := hex.DecodeString(encoded)

	return err == nil
}

func hasCacheDirective(values []string, expected string) bool {
	for _, value := range values {
		for _, directive := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(directive), expected) {
				return true
			}
		}
	}

	return false
}

func safeContentFilename(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || strings.ContainsAny(value, `/\:`) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}

	return true
}
