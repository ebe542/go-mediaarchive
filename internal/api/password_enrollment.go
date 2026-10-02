package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	apppasswords "github.com/ebe542/go-mediaarchive/internal/application/passwords"
	"github.com/ebe542/go-mediaarchive/internal/credential"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	"github.com/ebe542/go-mediaarchive/internal/password"
)

// PasswordEnrollmentService issues tokens and creates initial credentials.
type PasswordEnrollmentService interface {
	IssueEnrollment(
		ctx context.Context,
		userID string,
	) (apppasswords.IssuedEnrollment, error)

	CompleteEnrollment(
		ctx context.Context,
		token string,
		password []byte,
	) error
}

// PasswordEnrollmentAttemptLimiter limits public attempts by source IP.
type PasswordEnrollmentAttemptLimiter interface {
	Allow(sourceIP string, now time.Time) bool
	RecordFailure(sourceIP string, now time.Time)
	RecordSuccess(sourceIP string)
	Cancel(sourceIP string)
}

// WithPasswordEnrollmentAPI enables password enrollment HTTP endpoints.
func WithPasswordEnrollmentAPI(
	resolver SessionResolver,
	service PasswordEnrollmentService,
	limiter PasswordEnrollmentAttemptLimiter,
	clock Clock,
) Option {
	return func(configuration *handlerConfiguration) {
		configuration.passwordEnrollmentResolver = resolver
		configuration.passwordEnrollments = service
		configuration.passwordEnrollmentLimiter = limiter
		configuration.passwordEnrollmentClock = clock
	}
}

type passwordEnrollmentHandler struct {
	service PasswordEnrollmentService
	limiter PasswordEnrollmentAttemptLimiter
	clock   Clock
}

func (handler *passwordEnrollmentHandler) issue(
	response http.ResponseWriter,
	request *http.Request,
) {
	issued, err := handler.service.IssueEnrollment(
		request.Context(),
		request.PathValue("id"),
	)
	if err != nil {
		writePasswordEnrollmentIssueError(response, err)

		return
	}

	response.Header().Set(
		"Content-Type",
		"application/json; charset=utf-8",
	)
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusCreated)

	_ = json.NewEncoder(response).Encode(struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expiresAt"`
	}{
		Token:     issued.Token,
		ExpiresAt: issued.ExpiresAt,
	})
}

func (handler *passwordEnrollmentHandler) complete(
	response http.ResponseWriter,
	request *http.Request,
) {
	var requestBody struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := decodeJSONRequest(
		response,
		request,
		&requestBody,
	); err != nil || requestBody.Token == "" || requestBody.Password == "" {
		writeInvalidRequest(response)

		return
	}

	sourceIP, err := sourceIPAddress(request.RemoteAddr)
	if err != nil {
		writeInvalidRequest(response)

		return
	}

	currentTime := handler.clock().UTC()
	if !handler.limiter.Allow(sourceIP, currentTime) {
		writeJSONError(
			response,
			http.StatusTooManyRequests,
			"too_many_requests",
			"Too many password enrollment attempts.",
		)

		return
	}

	passwordBytes := []byte(requestBody.Password)
	defer clearBytes(passwordBytes)

	err = handler.service.CompleteEnrollment(
		request.Context(),
		requestBody.Token,
		passwordBytes,
	)
	if errors.Is(err, apppasswords.ErrInvalidEnrollment) {
		handler.limiter.RecordFailure(sourceIP, currentTime)
		writeJSONError(
			response,
			http.StatusUnauthorized,
			"invalid_enrollment",
			"Invalid password enrollment.",
		)

		return
	}
	if errors.Is(err, password.ErrInvalidPassword) {
		handler.limiter.Cancel(sourceIP)
		writeInvalidRequest(response)

		return
	}
	if err != nil {
		handler.limiter.Cancel(sourceIP)
		writeJSONError(
			response,
			http.StatusInternalServerError,
			"internal_error",
			"Internal server error.",
		)

		return
	}

	handler.limiter.RecordSuccess(sourceIP)
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func writePasswordEnrollmentIssueError(
	response http.ResponseWriter,
	inputError error,
) {
	switch {
	case errors.Is(inputError, identity.ErrInvalidUserID):
		writeInvalidRequest(response)
	case errors.Is(inputError, identity.ErrUserNotFound):
		writeJSONError(
			response,
			http.StatusNotFound,
			"not_found",
			"Resource not found.",
		)
	case errors.Is(inputError, credential.ErrPasswordCredentialExists):
		writeJSONError(
			response,
			http.StatusConflict,
			"credential_exists",
			"A password credential already exists.",
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
