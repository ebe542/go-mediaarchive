package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	appusers "github.com/ebe542/go-mediaarchive/internal/application/users"
)

// UserLister retrieves bounded pages from the user directory.
type UserLister interface {
	ListUsers(
		argContext context.Context,
		argInput appusers.ListUsersInput,
	) (appusers.UserPage, error)
}

// WithUserDirectoryAPI enables administrator-only user listing.
func WithUserDirectoryAPI(
	argResolver SessionResolver,
	argUserLister UserLister,
) Option {
	return func(argConfiguration *handlerConfiguration) {
		argConfiguration.userDirectoryResolver = argResolver
		argConfiguration.userLister = argUserLister
	}
}

type userDirectoryHandler struct {
	users UserLister
}

type userCursorDocument struct {
	CreatedAt string `json:"createdAt"`
	ID        string `json:"id"`
}

func (handler *userDirectoryHandler) listUsers(
	argResponse http.ResponseWriter,
	argRequest *http.Request,
) {
	input, err := parseUserDirectoryQuery(argRequest.URL.Query())
	if err != nil {
		writeInvalidRequest(argResponse)

		return
	}

	page, err := handler.users.ListUsers(argRequest.Context(), input)
	if errors.Is(err, appusers.ErrInvalidPageLimit) ||
		errors.Is(err, appusers.ErrInvalidCursor) {
		writeInvalidRequest(argResponse)

		return
	}
	if err != nil {
		writeJSONError(
			argResponse,
			http.StatusInternalServerError,
			"internal_error",
			"Internal server error.",
		)

		return
	}

	responseBody := struct {
		Users      []userResponse `json:"users"`
		NextCursor string         `json:"nextCursor,omitempty"`
	}{
		Users: make([]userResponse, len(page.Users)),
	}
	for index, user := range page.Users {
		responseBody.Users[index] = newUserResponse(user)
	}

	if page.NextCursor != nil {
		responseBody.NextCursor, err = encodeUserCursor(*page.NextCursor)
		if err != nil {
			writeJSONError(
				argResponse,
				http.StatusInternalServerError,
				"internal_error",
				"Internal server error.",
			)

			return
		}
	}

	argResponse.Header().Set(
		"Content-Type",
		"application/json; charset=utf-8",
	)
	argResponse.Header().Set("Cache-Control", "no-store")
	argResponse.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(argResponse).Encode(responseBody)
}

func parseUserDirectoryQuery(
	argValues url.Values,
) (appusers.ListUsersInput, error) {
	for name, values := range argValues {
		if name != "limit" && name != "cursor" {
			return appusers.ListUsersInput{}, errors.New(
				"unexpected user-directory query parameter",
			)
		}
		if len(values) != 1 {
			return appusers.ListUsersInput{}, errors.New(
				"expected one value per user-directory query parameter",
			)
		}
	}

	input := appusers.ListUsersInput{}
	if values, exists := argValues["limit"]; exists {
		limit, err := strconv.Atoi(values[0])
		if err != nil || limit < 1 || limit > appusers.MaximumPageLimit {
			return appusers.ListUsersInput{}, appusers.ErrInvalidPageLimit
		}

		input.Limit = limit
	}

	if values, exists := argValues["cursor"]; exists {
		cursor, err := decodeUserCursor(values[0])
		if err != nil {
			return appusers.ListUsersInput{}, err
		}

		input.Cursor = &cursor
	}

	return input, nil
}

func encodeUserCursor(argCursor appusers.Cursor) (string, error) {
	validatedCursor, err := appusers.NewCursor(
		argCursor.CreatedAt,
		argCursor.ID,
	)
	if err != nil {
		return "", err
	}

	document, err := json.Marshal(userCursorDocument{
		CreatedAt: validatedCursor.CreatedAt.Format(time.RFC3339Nano),
		ID:        validatedCursor.ID,
	})
	if err != nil {
		return "", fmt.Errorf("marshal user cursor: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(document), nil
}

func decodeUserCursor(argEncoded string) (appusers.Cursor, error) {
	if argEncoded == "" {
		return appusers.Cursor{}, appusers.ErrInvalidCursor
	}

	document, err := base64.RawURLEncoding.DecodeString(argEncoded)
	if err != nil {
		return appusers.Cursor{}, fmt.Errorf(
			"%w: decode Base64URL: %v",
			appusers.ErrInvalidCursor,
			err,
		)
	}

	cursorDocument, err := decodeUserCursorDocument(document)
	if err != nil {
		return appusers.Cursor{}, err
	}

	createdAt, err := time.Parse(time.RFC3339Nano, cursorDocument.CreatedAt)
	if err != nil ||
		cursorDocument.CreatedAt != createdAt.UTC().Format(time.RFC3339Nano) {
		return appusers.Cursor{}, fmt.Errorf(
			"%w: expected canonical UTC creation time",
			appusers.ErrInvalidCursor,
		)
	}

	cursor, err := appusers.NewCursor(createdAt, cursorDocument.ID)
	if err != nil {
		return appusers.Cursor{}, err
	}

	return cursor, nil
}

func decodeUserCursorDocument(
	argDocument []byte,
) (userCursorDocument, error) {
	decoder := json.NewDecoder(bytes.NewReader(argDocument))

	openingToken, err := decoder.Token()
	if err != nil || openingToken != json.Delim('{') {
		return userCursorDocument{}, fmt.Errorf(
			"%w: expected JSON object",
			appusers.ErrInvalidCursor,
		)
	}

	document := userCursorDocument{}
	seenFields := make(map[string]bool, 2)
	for decoder.More() {
		fieldToken, err := decoder.Token()
		if err != nil {
			return userCursorDocument{}, fmt.Errorf(
				"%w: decode field name: %v",
				appusers.ErrInvalidCursor,
				err,
			)
		}

		fieldName, ok := fieldToken.(string)
		if !ok || seenFields[fieldName] {
			return userCursorDocument{}, fmt.Errorf(
				"%w: duplicate cursor field",
				appusers.ErrInvalidCursor,
			)
		}
		seenFields[fieldName] = true

		switch fieldName {
		case "createdAt":
			err = decoder.Decode(&document.CreatedAt)
		case "id":
			err = decoder.Decode(&document.ID)
		default:
			return userCursorDocument{}, fmt.Errorf(
				"%w: unexpected cursor field",
				appusers.ErrInvalidCursor,
			)
		}
		if err != nil {
			return userCursorDocument{}, fmt.Errorf(
				"%w: decode cursor field: %v",
				appusers.ErrInvalidCursor,
				err,
			)
		}
	}

	closingToken, err := decoder.Token()
	if err != nil || closingToken != json.Delim('}') {
		return userCursorDocument{}, fmt.Errorf(
			"%w: expected cursor object end",
			appusers.ErrInvalidCursor,
		)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return userCursorDocument{}, fmt.Errorf(
			"%w: %v",
			appusers.ErrInvalidCursor,
			err,
		)
	}

	return document, nil
}
