package api

import (
	"context"
	"errors"
	"net/http"

	apppasswords "github.com/ebe542/go-mediaarchive/internal/application/passwords"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	"github.com/ebe542/go-mediaarchive/internal/password"
)

// PasswordChangeService verifies and replaces an authenticated user's password.
type PasswordChangeService interface {
	ChangePassword(
		ctx context.Context,
		actor identity.User,
		currentPassword []byte,
		newPassword []byte,
	) error
}

// WithPasswordChangeAPI enables authenticated self-service password changes.
func WithPasswordChangeAPI(
	resolver SessionResolver,
	service PasswordChangeService,
) Option {
	return func(configuration *handlerConfiguration) {
		configuration.passwordChangeResolver = resolver
		configuration.passwordChanges = service
	}
}

type passwordChangeHandler struct {
	service PasswordChangeService
}

func (handler *passwordChangeHandler) changeCurrentUserPassword(
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

	var requestBody struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := decodeJSONRequest(
		response,
		request,
		&requestBody,
	); err != nil ||
		requestBody.CurrentPassword == "" ||
		requestBody.NewPassword == "" {
		writeInvalidRequest(response)

		return
	}

	currentPassword := []byte(requestBody.CurrentPassword)
	newPassword := []byte(requestBody.NewPassword)
	defer clearBytes(currentPassword)
	defer clearBytes(newPassword)

	err := handler.service.ChangePassword(
		request.Context(),
		user,
		currentPassword,
		newPassword,
	)
	if errors.Is(err, apppasswords.ErrInvalidCurrentPassword) {
		writeJSONError(
			response,
			http.StatusUnauthorized,
			"invalid_credentials",
			"Invalid current password.",
		)

		return
	}
	if errors.Is(err, password.ErrInvalidPassword) ||
		errors.Is(err, apppasswords.ErrPasswordUnchanged) {
		writeInvalidRequest(response)

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

	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}
