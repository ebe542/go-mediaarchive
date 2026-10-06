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
		ctx context.Context,
		actor identity.User,
		input appusers.CreateUserInput,
	) (identity.User, error)
	UpdateUser(
		ctx context.Context,
		actor identity.User,
		id string,
		input appusers.UpdateUserInput,
	) (identity.User, error)
	SetUserActive(
		ctx context.Context,
		actor identity.User,
		id string,
		active bool,
	) (identity.User, error)
	DeleteUser(
		ctx context.Context,
		actorID string,
		id string,
	) error
}

// WithUserManagementAPI enables administrator-only user mutation endpoints.
func WithUserManagementAPI(
	resolver SessionResolver,
	userWriter UserWriter,
) Option {
	return func(configuration *handlerConfiguration) {
		configuration.sessionResolver = resolver
		configuration.userWriter = userWriter
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
	response http.ResponseWriter,
	request *http.Request,
) {
	actor, exists := AuthenticatedUser(request.Context())
	if !exists {
		writeJSONError(
			response,
			http.StatusInternalServerError,
			"internal_error",
			"Internal server error.",
		)

		return
	}

	var requestBody userDetailsRequest
	if err := decodeJSONRequest(
		response,
		request,
		&requestBody,
	); err != nil {
		writeInvalidRequest(response)

		return
	}

	createdUser, err := handler.users.CreateUser(
		request.Context(),
		actor,
		appusers.CreateUserInput{
			Username:    requestBody.Username,
			DisplayName: requestBody.DisplayName,
			Role:        requestBody.Role,
		},
	)
	if err != nil {
		writeUserApplicationError(response, err)

		return
	}

	response.Header().Set(
		"Location",
		"/api/v1/users/"+createdUser.ID,
	)
	writeUserResponseWithStatus(
		response,
		createdUser,
		http.StatusCreated,
	)
}

func (handler *userWriteHandler) updateUser(
	response http.ResponseWriter,
	request *http.Request,
) {
	actor, exists := AuthenticatedUser(request.Context())
	if !exists {
		writeJSONError(
			response,
			http.StatusInternalServerError,
			"internal_error",
			"Internal server error.",
		)

		return
	}

	var requestBody userDetailsRequest
	if err := decodeJSONRequest(
		response,
		request,
		&requestBody,
	); err != nil {
		writeInvalidRequest(response)

		return
	}

	updatedUser, err := handler.users.UpdateUser(
		request.Context(),
		actor,
		request.PathValue("id"),
		appusers.UpdateUserInput{
			Username:    requestBody.Username,
			DisplayName: requestBody.DisplayName,
			Role:        requestBody.Role,
		},
	)
	if err != nil {
		writeUserApplicationError(response, err)

		return
	}

	writeUserResponse(response, updatedUser)
}

func (handler *userWriteHandler) setUserActive(
	response http.ResponseWriter,
	request *http.Request,
) {
	actor, exists := AuthenticatedUser(request.Context())
	if !exists {
		writeJSONError(
			response,
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
		response,
		request,
		&requestBody,
	); err != nil || requestBody.Active == nil {
		writeInvalidRequest(response)

		return
	}

	updatedUser, err := handler.users.SetUserActive(
		request.Context(),
		actor,
		request.PathValue("id"),
		*requestBody.Active,
	)
	if err != nil {
		writeUserApplicationError(response, err)

		return
	}

	writeUserResponse(response, updatedUser)
}

func (handler *userWriteHandler) deleteUser(
	response http.ResponseWriter,
	request *http.Request,
) {
	actor, exists := AuthenticatedUser(request.Context())
	if !exists {
		writeJSONError(
			response,
			http.StatusInternalServerError,
			"internal_error",
			"Internal server error.",
		)

		return
	}

	if err := handler.users.DeleteUser(
		request.Context(),
		actor.ID,
		request.PathValue("id"),
	); err != nil {
		writeUserApplicationError(response, err)

		return
	}

	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func writeInvalidRequest(response http.ResponseWriter) {
	writeJSONError(
		response,
		http.StatusBadRequest,
		"invalid_request",
		"Invalid request.",
	)
}

func writeUserApplicationError(
	response http.ResponseWriter,
	inputError error,
) {
	switch {
	case isInvalidUserInput(inputError):
		writeInvalidRequest(response)
	case errors.Is(inputError, identity.ErrUserNotFound):
		writeJSONError(
			response,
			http.StatusNotFound,
			"not_found",
			"Resource not found.",
		)
	case errors.Is(inputError, identity.ErrUserConflict):
		writeJSONError(
			response,
			http.StatusConflict,
			"conflict",
			"User identity conflicts with an existing resource.",
		)
	case errors.Is(inputError, appusers.ErrSelfLockout):
		writeJSONError(
			response,
			http.StatusConflict,
			"self_lockout",
			"An administrator cannot remove their own access.",
		)
	case errors.Is(inputError, appusers.ErrSelfDeletion):
		writeJSONError(
			response,
			http.StatusConflict,
			"self_deletion",
			"An administrator cannot delete their own identity.",
		)
	case errors.Is(inputError, identity.ErrLastAdministrator):
		writeJSONError(
			response,
			http.StatusConflict,
			"last_administrator",
			"The last active administrator must be preserved.",
		)
	case errors.Is(inputError, identity.ErrUserOwnsMedia):
		writeJSONError(
			response,
			http.StatusConflict,
			"owned_media",
			"The user owns media that must be transferred or deleted first.",
		)
	default:
		writeJSONError(
			response,
			http.StatusInternalServerError,
			"internal_error",
			"Internal server error.",
		)
	}
}

func isInvalidUserInput(inputError error) bool {
	return errors.Is(inputError, identity.ErrInvalidUserID) ||
		errors.Is(inputError, identity.ErrInvalidUsername) ||
		errors.Is(inputError, identity.ErrInvalidDisplayName) ||
		errors.Is(inputError, identity.ErrInvalidRole) ||
		errors.Is(inputError, identity.ErrInvalidTimestamp)
}
