package api

import (
	"context"
	"errors"
	"net/http"

	appusers "github.com/ebe542/go-mediaarchive/internal/application/users"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

// UserWriter performs administrator-controlled user identity mutations.
type UserWriter interface {
	CreateUser(
		argContext context.Context,
		argInput appusers.CreateUserInput,
	) (identity.User, error)
	UpdateUser(
		argContext context.Context,
		argActorID string,
		argID string,
		argInput appusers.UpdateUserInput,
	) (identity.User, error)
	SetUserActive(
		argContext context.Context,
		argActorID string,
		argID string,
		argActive bool,
	) (identity.User, error)
}

// WithUserManagementAPI enables administrator-only user mutation endpoints.
func WithUserManagementAPI(
	argResolver SessionResolver,
	argUserWriter UserWriter,
) Option {
	return func(argConfiguration *handlerConfiguration) {
		argConfiguration.sessionResolver = argResolver
		argConfiguration.userWriter = argUserWriter
	}
}

type userWriteHandler struct {
	users UserWriter
}

type userDetailsRequest struct {
	Username    string        `json:"username"`
	DisplayName string        `json:"displayName"`
	Role        identity.Role `json:"role"`
}

func (handler *userWriteHandler) createUser(
	argResponse http.ResponseWriter,
	argRequest *http.Request,
) {
	var requestBody userDetailsRequest
	if err := decodeJSONRequest(
		argResponse,
		argRequest,
		&requestBody,
	); err != nil {
		writeInvalidRequest(argResponse)

		return
	}

	createdUser, err := handler.users.CreateUser(
		argRequest.Context(),
		appusers.CreateUserInput{
			Username:    requestBody.Username,
			DisplayName: requestBody.DisplayName,
			Role:        requestBody.Role,
		},
	)
	if err != nil {
		writeUserApplicationError(argResponse, err)

		return
	}

	argResponse.Header().Set(
		"Location",
		"/api/v1/users/"+createdUser.ID,
	)
	writeUserResponseWithStatus(
		argResponse,
		createdUser,
		http.StatusCreated,
	)
}

func (handler *userWriteHandler) updateUser(
	argResponse http.ResponseWriter,
	argRequest *http.Request,
) {
	actor, exists := AuthenticatedUser(argRequest.Context())
	if !exists {
		writeJSONError(
			argResponse,
			http.StatusInternalServerError,
			"internal_error",
			"Internal server error.",
		)

		return
	}

	var requestBody userDetailsRequest
	if err := decodeJSONRequest(
		argResponse,
		argRequest,
		&requestBody,
	); err != nil {
		writeInvalidRequest(argResponse)

		return
	}

	updatedUser, err := handler.users.UpdateUser(
		argRequest.Context(),
		actor.ID,
		argRequest.PathValue("id"),
		appusers.UpdateUserInput{
			Username:    requestBody.Username,
			DisplayName: requestBody.DisplayName,
			Role:        requestBody.Role,
		},
	)
	if err != nil {
		writeUserApplicationError(argResponse, err)

		return
	}

	writeUserResponse(argResponse, updatedUser)
}

func (handler *userWriteHandler) setUserActive(
	argResponse http.ResponseWriter,
	argRequest *http.Request,
) {
	actor, exists := AuthenticatedUser(argRequest.Context())
	if !exists {
		writeJSONError(
			argResponse,
			http.StatusInternalServerError,
			"internal_error",
			"Internal server error.",
		)

		return
	}

	var requestBody struct {
		Active *bool `json:"active"`
	}
	if err := decodeJSONRequest(
		argResponse,
		argRequest,
		&requestBody,
	); err != nil || requestBody.Active == nil {
		writeInvalidRequest(argResponse)

		return
	}

	updatedUser, err := handler.users.SetUserActive(
		argRequest.Context(),
		actor.ID,
		argRequest.PathValue("id"),
		*requestBody.Active,
	)
	if err != nil {
		writeUserApplicationError(argResponse, err)

		return
	}

	writeUserResponse(argResponse, updatedUser)
}

func writeInvalidRequest(argResponse http.ResponseWriter) {
	writeJSONError(
		argResponse,
		http.StatusBadRequest,
		"invalid_request",
		"Invalid request.",
	)
}

func writeUserApplicationError(
	argResponse http.ResponseWriter,
	argError error,
) {
	switch {
	case isInvalidUserInput(argError):
		writeInvalidRequest(argResponse)
	case errors.Is(argError, identity.ErrUserNotFound):
		writeJSONError(
			argResponse,
			http.StatusNotFound,
			"not_found",
			"Resource not found.",
		)
	case errors.Is(argError, identity.ErrUserConflict):
		writeJSONError(
			argResponse,
			http.StatusConflict,
			"conflict",
			"User identity conflicts with an existing resource.",
		)
	case errors.Is(argError, appusers.ErrSelfLockout):
		writeJSONError(
			argResponse,
			http.StatusConflict,
			"self_lockout",
			"An administrator cannot remove their own access.",
		)
	case errors.Is(argError, identity.ErrLastAdministrator):
		writeJSONError(
			argResponse,
			http.StatusConflict,
			"last_administrator",
			"The last active administrator must be preserved.",
		)
	default:
		writeJSONError(
			argResponse,
			http.StatusInternalServerError,
			"internal_error",
			"Internal server error.",
		)
	}
}

func isInvalidUserInput(argError error) bool {
	return errors.Is(argError, identity.ErrInvalidUserID) ||
		errors.Is(argError, identity.ErrInvalidUsername) ||
		errors.Is(argError, identity.ErrInvalidDisplayName) ||
		errors.Is(argError, identity.ErrInvalidRole) ||
		errors.Is(argError, identity.ErrInvalidTimestamp)
}
