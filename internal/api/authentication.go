package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/application/authentication"
	appsessions "github.com/ebe542/go-mediaarchive/internal/application/sessions"
)

// SessionService creates and revokes authenticated sessions.
type SessionService interface {
	Create(
		ctx context.Context,
		username string,
		password []byte,
	) (appsessions.Created, error)

	Revoke(
		ctx context.Context,
		accessToken string,
	) error
}

// AttemptLimiter limits failed login attempts.
type AttemptLimiter interface {
	Allow(
		username string,
		sourceIP string,
		now time.Time,
	) bool

	RecordFailure(
		username string,
		sourceIP string,
		now time.Time,
	)

	RecordSuccess(
		username string,
		sourceIP string,
	)

	Cancel(
		username string,
		sourceIP string,
	)
}

// Clock returns the current API time.
type Clock func() time.Time

type handlerConfiguration struct {
	sessions                   SessionService
	limiter                    AttemptLimiter
	clock                      Clock
	sessionResolver            SessionResolver
	userReader                 UserReader
	userWriter                 UserWriter
	passwordEnrollmentResolver SessionResolver
	passwordEnrollments        PasswordEnrollmentService
	passwordEnrollmentLimiter  PasswordEnrollmentAttemptLimiter
	passwordEnrollmentClock    Clock
	passwordChangeResolver     SessionResolver
	passwordChanges            PasswordChangeService
	userDirectoryResolver      SessionResolver
	userLister                 UserLister
	mediaResolver              SessionResolver
	mediaMetadata              MediaMetadataService
	mediaGrantResolver         SessionResolver
	mediaGrants                MediaGrantService
	mediaUploadResolver        SessionResolver
	mediaUploads               MediaUploadService
	maximumUploadSize          int64
}

// Option configures optional API capabilities.
type Option func(configuration *handlerConfiguration)

// WithAuthentication enables the authentication session endpoints.
func WithAuthentication(
	sessions SessionService,
	limiter AttemptLimiter,
	clock Clock,
) Option {
	return func(configuration *handlerConfiguration) {
		configuration.sessions = sessions
		configuration.limiter = limiter
		configuration.clock = clock
	}
}

type authenticationHandler struct {
	sessions SessionService
	limiter  AttemptLimiter
	clock    Clock
}

type errorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (handler *authenticationHandler) createSession(
	response http.ResponseWriter,
	request *http.Request,
) {
	var requestBody struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := decodeJSONRequest(
		response,
		request,
		&requestBody,
	); err != nil {
		writeJSONError(
			response,
			http.StatusBadRequest,
			"invalid_request",
			"Invalid request.",
		)

		return
	}

	if requestBody.Username == "" ||
		requestBody.Password == "" {
		writeJSONError(
			response,
			http.StatusBadRequest,
			"invalid_request",
			"Invalid request.",
		)

		return
	}

	sourceIP, err := sourceIPAddress(request.RemoteAddr)
	if err != nil {
		writeJSONError(
			response,
			http.StatusBadRequest,
			"invalid_request",
			"Invalid request.",
		)

		return
	}

	currentTime := handler.clock().UTC()

	if !handler.limiter.Allow(
		requestBody.Username,
		sourceIP,
		currentTime,
	) {
		writeJSONError(
			response,
			http.StatusTooManyRequests,
			"too_many_requests",
			"Too many authentication attempts.",
		)

		return
	}

	passwordBytes := []byte(requestBody.Password)
	defer clearBytes(passwordBytes)

	createdSession, err := handler.sessions.Create(
		request.Context(),
		requestBody.Username,
		passwordBytes,
	)
	if errors.Is(err, authentication.ErrInvalidCredentials) {
		handler.limiter.RecordFailure(
			requestBody.Username,
			sourceIP,
			currentTime,
		)

		writeJSONError(
			response,
			http.StatusUnauthorized,
			"invalid_credentials",
			"Invalid username or password.",
		)

		return
	}
	if err != nil {
		handler.limiter.Cancel(
			requestBody.Username,
			sourceIP,
		)

		writeJSONError(
			response,
			http.StatusInternalServerError,
			"internal_error",
			"Internal server error.",
		)

		return
	}

	handler.limiter.RecordSuccess(requestBody.Username, sourceIP)

	response.Header().Set(
		"Content-Type",
		"application/json; charset=utf-8",
	)
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusCreated)

	_ = json.NewEncoder(response).Encode(struct {
		AccessToken string    `json:"accessToken"`
		TokenType   string    `json:"tokenType"`
		ExpiresAt   time.Time `json:"expiresAt"`
	}{
		AccessToken: createdSession.AccessToken,
		TokenType:   "Bearer",
		ExpiresAt:   createdSession.ExpiresAt,
	})
}

func sourceIPAddress(remoteAddress string) (string, error) {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		return "", fmt.Errorf("parse remote address: %w", err)
	}

	return host, nil
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}

	// Keep the slice alive until clearing has completed.
	runtime.KeepAlive(value)
}

func writeJSONError(
	response http.ResponseWriter,
	status int,
	code string,
	message string,
) {
	responseBody := errorResponse{}
	responseBody.Error.Code = code
	responseBody.Error.Message = message

	response.Header().Set(
		"Content-Type",
		"application/json; charset=utf-8",
	)
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)

	_ = json.NewEncoder(response).Encode(responseBody)
}

func (handler *authenticationHandler) revokeCurrentSession(
	response http.ResponseWriter,
	request *http.Request,
) {
	accessToken, err := bearerToken(
		request.Header.Values("Authorization"),
	)
	if err != nil {
		writeJSONError(
			response,
			http.StatusUnauthorized,
			"authentication_required",
			"Authentication required.",
		)

		return
	}

	if err := handler.sessions.Revoke(
		request.Context(),
		accessToken,
	); err != nil {
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

func bearerToken(authorizationHeaders []string) (string, error) {
	if len(authorizationHeaders) != 1 {
		return "", errors.New(
			"expected exactly one Authorization header",
		)
	}

	parts := strings.Fields(authorizationHeaders[0])
	if len(parts) != 2 ||
		!strings.EqualFold(parts[0], "Bearer") ||
		parts[1] == "" {
		return "", errors.New("expected a bearer token")
	}

	return parts[1], nil
}
