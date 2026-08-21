package api

import (
	"context"
	"errors"
	"net/http"
	"slices"

	appsessions "github.com/ebe542/go-mediaarchive/internal/application/sessions"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

// SessionResolver resolves an authenticated user from an opaque access token.
type SessionResolver interface {
	Resolve(
		argContext context.Context,
		argAccessToken string,
	) (identity.User, error)
}

type authenticatedUserContextKey struct{}

// AuthenticatedUser returns the authenticated user stored in a request context.
func AuthenticatedUser(
	argContext context.Context,
) (identity.User, bool) {
	user, exists := argContext.Value(
		authenticatedUserContextKey{},
	).(identity.User)

	return user, exists
}

// RequireAuthentication resolves a bearer token before calling a protected handler.
func RequireAuthentication(
	argResolver SessionResolver,
	argNext http.Handler,
) http.Handler {
	return http.HandlerFunc(func(
		argResponse http.ResponseWriter,
		argRequest *http.Request,
	) {
		accessToken, err := bearerToken(
			argRequest.Header.Values("Authorization"),
		)
		if err != nil {
			writeAuthenticationRequired(argResponse)

			return
		}

		user, err := argResolver.Resolve(
			argRequest.Context(),
			accessToken,
		)
		if errors.Is(err, appsessions.ErrUnauthenticated) {
			writeAuthenticationRequired(argResponse)

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
		requestContext := context.WithValue(
			argRequest.Context(),
			authenticatedUserContextKey{},
			user,
		)

		argNext.ServeHTTP(
			argResponse,
			argRequest.WithContext(requestContext),
		)
	})
}

func writeAuthenticationRequired(
	argResponse http.ResponseWriter,
) {
	argResponse.Header().Set(
		"WWW-Authenticate",
		"Bearer",
	)
	writeJSONError(
		argResponse,
		http.StatusUnauthorized,
		"authentication_required",
		"Authentication required.",
	)
}

// RequireRoles permits a request when its authenticated user has an allowed role.
func RequireRoles(
	argNext http.Handler,
	argAllowedRoles ...identity.Role,
) http.Handler {
	return http.HandlerFunc(func(
		argResponse http.ResponseWriter,
		argRequest *http.Request,
	) {
		user, exists := AuthenticatedUser(argRequest.Context())
		if !exists {
			writeAuthenticationRequired(argResponse)

			return
		}

		if slices.Contains(argAllowedRoles, user.Role) {
			argNext.ServeHTTP(argResponse, argRequest)

			return
		}

		writeJSONError(
			argResponse,
			http.StatusForbidden,
			"forbidden",
			"Access forbidden.",
		)
	})
}
