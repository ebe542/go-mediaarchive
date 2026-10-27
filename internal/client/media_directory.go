package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

const (
	defaultMediaDirectoryLimit = 50
	maximumMediaDirectoryLimit = 100
	maximumMediaQueryLength    = 200
	maximumMediaCursorLength   = 1024
)

// MediaDirectoryFilter selects one bounded discoverable-media page.
type MediaDirectoryFilter struct {
	Query  string
	Type   domainmedia.Type
	Limit  int
	Cursor string
}

// MediaPage contains one discoverable-media page and its continuation token.
type MediaPage struct {
	Items      []Media
	NextCursor string
}

type mediaPageResponse struct {
	Items      []mediaResponse `json:"items"`
	NextCursor string          `json:"nextCursor,omitempty"`
}

// ListMedia returns one authenticated discoverable-media page.
func (client *Client) ListMedia(
	ctx context.Context,
	accessToken string,
	filter MediaDirectoryFilter,
) (MediaPage, error) {
	query, limit, err := mediaDirectoryQuery(filter)
	if err != nil {
		return MediaPage{}, err
	}
	path := "/api/v1/media"
	if encodedQuery := query.Encode(); encodedQuery != "" {
		path += "?" + encodedQuery
	}

	var response mediaPageResponse
	if err := client.doJSON(
		ctx,
		http.MethodGet,
		path,
		accessToken,
		nil,
		http.StatusOK,
		&response,
		"media directory",
	); err != nil {
		return MediaPage{}, err
	}

	return decodeMediaPage(response, limit)
}

func mediaDirectoryQuery(filter MediaDirectoryFilter) (url.Values, int, error) {
	query := url.Values{}
	filter.Query = strings.TrimSpace(filter.Query)
	if utf8.RuneCountInString(filter.Query) > maximumMediaQueryLength {
		return nil, 0, fmt.Errorf(
			"validate media directory filter: query exceeds %d characters",
			maximumMediaQueryLength,
		)
	}
	for _, character := range filter.Query {
		if unicode.IsControl(character) {
			return nil, 0, errors.New(
				"validate media directory filter: query contains a control character",
			)
		}
	}
	if filter.Query != "" {
		query.Set("query", filter.Query)
	}
	if filter.Type != "" {
		if !filter.Type.Valid() {
			return nil, 0, errors.New(
				"validate media directory filter: unsupported media type",
			)
		}
		query.Set("type", string(filter.Type))
	}

	limit := filter.Limit
	if limit == 0 {
		limit = defaultMediaDirectoryLimit
	} else {
		if limit < 1 || limit > maximumMediaDirectoryLimit {
			return nil, 0, fmt.Errorf(
				"validate media directory filter: limit must be from 1 through %d",
				maximumMediaDirectoryLimit,
			)
		}
		query.Set("limit", strconv.Itoa(limit))
	}
	if filter.Cursor != "" {
		if err := validateMediaCursor(filter.Cursor); err != nil {
			return nil, 0, err
		}
		query.Set("cursor", filter.Cursor)
	}

	return query, limit, nil
}

func decodeMediaPage(response mediaPageResponse, limit int) (MediaPage, error) {
	if response.Items == nil {
		return MediaPage{}, errors.New(
			"validate media directory response: items are required",
		)
	}
	if len(response.Items) > limit {
		return MediaPage{}, errors.New(
			"validate media directory response: too many items",
		)
	}
	if response.NextCursor != "" {
		if err := validateMediaCursor(response.NextCursor); err != nil {
			return MediaPage{}, fmt.Errorf(
				"validate media directory response cursor: %w",
				err,
			)
		}
		if len(response.Items) != limit {
			return MediaPage{}, errors.New(
				"validate media directory response: cursor requires a full page",
			)
		}
	}

	page := MediaPage{
		Items:      make([]Media, len(response.Items)),
		NextCursor: response.NextCursor,
	}
	seenIDs := make(map[string]struct{}, len(response.Items))
	for index, candidate := range response.Items {
		item, err := decodeMediaResponse(candidate, "media directory")
		if err != nil {
			return MediaPage{}, fmt.Errorf(
				"validate media directory item %d: %w",
				index,
				err,
			)
		}
		if _, exists := seenIDs[item.ID]; exists {
			return MediaPage{}, errors.New(
				"validate media directory response: duplicate media ID",
			)
		}
		seenIDs[item.ID] = struct{}{}
		if index > 0 && !mediaBefore(page.Items[index-1], item) {
			return MediaPage{}, errors.New(
				"validate media directory response: items are not in descending order",
			)
		}
		page.Items[index] = item
	}

	return page, nil
}

func validateMediaCursor(cursor string) error {
	if len(cursor) > maximumMediaCursorLength || cursor != strings.TrimSpace(cursor) {
		return errors.New("validate media cursor: expected bounded URL-safe text")
	}
	for _, character := range cursor {
		if !((character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '-' || character == '_') {
			return errors.New("validate media cursor: expected bounded URL-safe text")
		}
	}

	return nil
}

func mediaBefore(previous Media, current Media) bool {
	if previous.CreatedAt.Equal(current.CreatedAt) {
		return previous.ID > current.ID
	}

	return previous.CreatedAt.After(current.CreatedAt)
}
