package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/identity"
)

// UserReader retrieves user identities for read-only API operations.
type UserReader interface {
	UserByID(
		argContext context.Context,
		argID string,
	) (identity.User, error)
}

// WithUserReadAPI enables authenticated read-only user endpoints.
func WithUserReadAPI(
	argResolver SessionResolver,
	argUserReader UserReader,
) Option {
	return func(argConfiguration *handlerConfiguration) {
		argConfiguration.sessionResolver = argResolver
		argConfiguration.userReader = argUserReader
	}
}

type userReadHandler struct {
	users UserReader
}

type userResponse struct {
	ID          string        `json:"id"`
	Username    string        `json:"username"`
	DisplayName string        `json:"displayName"`
	Role        identity.Role `json:"role"`
	Active      bool          `json:"active"`
	CreatedAt   time.Time     `json:"createdAt"`
	UpdatedAt   time.Time     `json:"updatedAt"`
}

func (handler *userReadHandler) currentUser(
	argResponse http.ResponseWriter,
	argRequest *http.Request,
) {
	user, exists := AuthenticatedUser(argRequest.Context())
	if !exists {
		writeJSONError(
			argResponse,
			http.StatusInternalServerError,
			"internal_error",
			"Internal server error.",
		)

		return
	}

	writeUserResponse(argResponse, user)
}

func (handler *userReadHandler) userByID(
	argResponse http.ResponseWriter,
	argRequest *http.Request,
) {
	user, err := handler.users.UserByID(
		argRequest.Context(),
		argRequest.PathValue("id"),
	)
	if errors.Is(err, identity.ErrUserNotFound) {
		writeJSONError(
			argResponse,
			http.StatusNotFound,
			"not_found",
			"Resource not found.",
		)

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

	writeUserResponse(argResponse, user)
}

func writeUserResponse(
	argResponse http.ResponseWriter,
	argUser identity.User,
) {
	writeUserResponseWithStatus(
		argResponse,
		argUser,
		http.StatusOK,
	)
}

func writeUserResponseWithStatus(
	argResponse http.ResponseWriter,
	argUser identity.User,
	argStatus int,
) {
	argResponse.Header().Set(
		"Content-Type",
		"application/json; charset=utf-8",
	)
	argResponse.Header().Set("Cache-Control", "no-store")
	argResponse.WriteHeader(argStatus)

	_ = json.NewEncoder(argResponse).Encode(newUserResponse(argUser))
}

func newUserResponse(argUser identity.User) userResponse {
	return userResponse{
		ID:          argUser.ID,
		Username:    argUser.Username,
		DisplayName: argUser.DisplayName,
		Role:        argUser.Role,
		Active:      argUser.Active,
		CreatedAt:   argUser.CreatedAt,
		UpdatedAt:   argUser.UpdatedAt,
	}
}
