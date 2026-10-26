package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	appmedia "github.com/ebe542/go-mediaarchive/internal/application/media"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

const maximumMediaCursorLength = 1024

// MediaDirectoryLister retrieves bounded pages from the discoverable media directory.
type MediaDirectoryLister interface {
	ListItems(
		context.Context,
		identity.User,
		appmedia.ListItemsInput,
	) (appmedia.ItemPage, error)
}

// WithMediaDirectoryAPI enables authenticated media discovery.
func WithMediaDirectoryAPI(
	resolver SessionResolver,
	lister MediaDirectoryLister,
) Option {
	return func(configuration *handlerConfiguration) {
		configuration.mediaDirectoryResolver = resolver
		configuration.mediaDirectoryLister = lister
	}
}

type mediaDirectoryHandler struct {
	media MediaDirectoryLister
}

type mediaCursorDocument struct {
	Version    int    `json:"version"`
	CreatedAt  string `json:"createdAt"`
	ID         string `json:"id"`
	FilterHash string `json:"filterHash"`
	Limit      int    `json:"limit"`
}

func (handler *mediaDirectoryHandler) list(
	response http.ResponseWriter,
	request *http.Request,
) {
	input, err := parseMediaDirectoryQuery(request.URL.Query())
	if err != nil {
		writeInvalidRequest(response)

		return
	}
	actor, exists := AuthenticatedUser(request.Context())
	if !exists {
		writeMediaContextError(response)

		return
	}
	page, err := handler.media.ListItems(request.Context(), actor, input)
	if errors.Is(err, appmedia.ErrInvalidDirectoryFilter) ||
		errors.Is(err, appmedia.ErrInvalidDirectoryLimit) ||
		errors.Is(err, appmedia.ErrInvalidDirectoryCursor) {
		writeInvalidRequest(response)

		return
	}
	if errors.Is(err, appmedia.ErrDirectoryForbidden) {
		writeJSONError(response, http.StatusForbidden, "forbidden", "Access forbidden.")

		return
	}
	if err != nil {
		writeMediaContextError(response)

		return
	}

	body := struct {
		Items      []mediaMetadataResponse `json:"items"`
		NextCursor string                  `json:"nextCursor,omitempty"`
	}{Items: make([]mediaMetadataResponse, len(page.Items))}
	for index, item := range page.Items {
		body.Items[index] = newMediaMetadataResponse(item)
	}
	if page.NextCursor != nil {
		body.NextCursor, err = encodeMediaCursor(*page.NextCursor)
		if err != nil {
			writeMediaContextError(response)

			return
		}
	}

	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(response).Encode(body)
}

func parseMediaDirectoryQuery(values url.Values) (appmedia.ListItemsInput, error) {
	allowed := map[string]bool{
		"query": true, "type": true, "limit": true, "cursor": true,
	}
	for name, entries := range values {
		if !allowed[name] || len(entries) != 1 || entries[0] == "" {
			return appmedia.ListItemsInput{}, appmedia.ErrInvalidDirectoryFilter
		}
	}

	input := appmedia.ListItemsInput{Filter: appmedia.DirectoryFilter{
		Query: values.Get("query"),
		Type:  domainmedia.Type(values.Get("type")),
	}}
	if value := values.Get("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > appmedia.MaximumDirectoryLimit {
			return appmedia.ListItemsInput{}, appmedia.ErrInvalidDirectoryLimit
		}
		input.Limit = limit
	}
	if value := values.Get("cursor"); value != "" {
		cursor, err := decodeMediaCursor(value)
		if err != nil {
			return appmedia.ListItemsInput{}, err
		}
		input.Cursor = &cursor
	}

	return input, nil
}

func encodeMediaCursor(cursor appmedia.DirectoryCursor) (string, error) {
	validated, err := appmedia.NewDirectoryCursor(
		cursor.CreatedAt,
		cursor.ID,
		cursor.FilterHash,
		cursor.Limit,
	)
	if err != nil {
		return "", err
	}
	document, err := json.Marshal(mediaCursorDocument{
		Version:    1,
		CreatedAt:  validated.CreatedAt.Format(time.RFC3339Nano),
		ID:         validated.ID,
		FilterHash: base64.RawURLEncoding.EncodeToString(validated.FilterHash[:]),
		Limit:      validated.Limit,
	})
	if err != nil {
		return "", fmt.Errorf("marshal media cursor: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(document), nil
}

func decodeMediaCursor(encoded string) (appmedia.DirectoryCursor, error) {
	if len(encoded) > maximumMediaCursorLength {
		return appmedia.DirectoryCursor{}, appmedia.ErrInvalidDirectoryCursor
	}
	document, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return appmedia.DirectoryCursor{}, appmedia.ErrInvalidDirectoryCursor
	}
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()
	var decoded mediaCursorDocument
	if err := decoder.Decode(&decoded); err != nil {
		return appmedia.DirectoryCursor{}, appmedia.ErrInvalidDirectoryCursor
	}
	if err := ensureJSONEnd(decoder); err != nil || decoded.Version != 1 {
		return appmedia.DirectoryCursor{}, appmedia.ErrInvalidDirectoryCursor
	}
	createdAt, err := parseCanonicalUTCTime(decoded.CreatedAt)
	if err != nil {
		return appmedia.DirectoryCursor{}, appmedia.ErrInvalidDirectoryCursor
	}
	hashBytes, err := base64.RawURLEncoding.DecodeString(decoded.FilterHash)
	if err != nil || len(hashBytes) != sha256.Size {
		return appmedia.DirectoryCursor{}, appmedia.ErrInvalidDirectoryCursor
	}
	var filterHash [sha256.Size]byte
	copy(filterHash[:], hashBytes)
	cursor, err := appmedia.NewDirectoryCursor(
		*createdAt,
		decoded.ID,
		filterHash,
		decoded.Limit,
	)
	if err != nil {
		return appmedia.DirectoryCursor{}, err
	}
	canonical, err := encodeMediaCursor(cursor)
	if err != nil || canonical != encoded {
		return appmedia.DirectoryCursor{}, appmedia.ErrInvalidDirectoryCursor
	}

	return cursor, nil
}
