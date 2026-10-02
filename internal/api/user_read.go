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
		ctx context.Context,
		id string,
	) (identity.User, error)
}

// WithUserReadAPI enables authenticated read-only user endpoints.
func WithUserReadAPI(
	resolver SessionResolver,
	userReader UserReader,
) Option {
	return func(configuration *handlerConfiguration) {
		configuration.sessionResolver = resolver
		configuration.userReader = userReader
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
	response http.ResponseWriter,
	request *http.Request,
) {
	user, exists := AuthenticatedUser(request.Context())
	if !exists {
		writeJSONError(
			response,
			http.StatusInternalServerError,
			"internal_error",
			"Internal server error.",
		)

		return
	}

	writeUserResponse(response, user)
}

func (handler *userReadHandler) userByID(
	response http.ResponseWriter,
	request *http.Request,
) {
	user, err := handler.users.UserByID(
		request.Context(),
		request.PathValue("id"),
	)
	if errors.Is(err, identity.ErrUserNotFound) {
		writeJSONError(
			response,
			http.StatusNotFound,
			"not_found",
			"Resource not found.",
		)

		return
	}
	if err != nil {
		writeJSONError(
			response,
			http.StatusInternalServerError,
			"internal_error",
			"Internal server error.",
		)

		return
	}

	writeUserResponse(response, user)
}

func writeUserResponse(
	response http.ResponseWriter,
	user identity.User,
) {
	writeUserResponseWithStatus(
		response,
		user,
		http.StatusOK,
	)
}

func writeUserResponseWithStatus(
	response http.ResponseWriter,
	user identity.User,
	status int,
) {
	response.Header().Set(
		"Content-Type",
		"application/json; charset=utf-8",
	)
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)

	_ = json.NewEncoder(response).Encode(newUserResponse(user))
}

func newUserResponse(user identity.User) userResponse {
	return userResponse{
		ID:          user.ID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Role:        user.Role,
		Active:      user.Active,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
	}
}
