package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	appmedia "github.com/ebe542/go-mediaarchive/internal/application/media"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

// MediaGrantService performs authorized per-user media grant operations.
type MediaGrantService interface {
	ReplaceGrant(
		context.Context,
		identity.User,
		string,
		string,
		domainmedia.PermissionSet,
	) (domainmedia.Grant, error)
	GrantByUser(
		context.Context,
		identity.User,
		string,
		string,
	) (domainmedia.Grant, error)
	GrantsByMedia(
		context.Context,
		identity.User,
		string,
	) ([]domainmedia.Grant, error)
	RevokeGrant(context.Context, identity.User, string, string) error
}

// WithMediaGrantAPI enables authenticated per-user media grant endpoints.
func WithMediaGrantAPI(
	argResolver SessionResolver,
	argService MediaGrantService,
) Option {
	return func(argConfiguration *handlerConfiguration) {
		argConfiguration.mediaGrantResolver = argResolver
		argConfiguration.mediaGrants = argService
	}
}

type mediaGrantHandler struct {
	grants MediaGrantService
}

type mediaGrantRequest struct {
	Permissions []string `json:"permissions"`
}

type mediaGrantResponse struct {
	MediaID     string   `json:"mediaId"`
	UserID      string   `json:"userId"`
	Permissions []string `json:"permissions"`
}

func (handler *mediaGrantHandler) replace(
	argResponse http.ResponseWriter,
	argRequest *http.Request,
) {
	actor, exists := mediaActor(argRequest)
	if !exists {
		writeMediaContextError(argResponse)

		return
	}

	permissions, ok := decodePermissionSet(argResponse, argRequest)
	if !ok {
		return
	}
	grant, err := handler.grants.ReplaceGrant(
		argRequest.Context(),
		actor,
		argRequest.PathValue("id"),
		argRequest.PathValue("userId"),
		permissions,
	)
	if err != nil {
		writeMediaGrantApplicationError(argResponse, err)

		return
	}

	writeMediaGrantResponse(argResponse, grant)
}

func (handler *mediaGrantHandler) read(
	argResponse http.ResponseWriter,
	argRequest *http.Request,
) {
	actor, exists := mediaActor(argRequest)
	if !exists {
		writeMediaContextError(argResponse)

		return
	}

	grant, err := handler.grants.GrantByUser(
		argRequest.Context(),
		actor,
		argRequest.PathValue("id"),
		argRequest.PathValue("userId"),
	)
	if err != nil {
		writeMediaGrantApplicationError(argResponse, err)

		return
	}

	writeMediaGrantResponse(argResponse, grant)
}

func (handler *mediaGrantHandler) list(
	argResponse http.ResponseWriter,
	argRequest *http.Request,
) {
	actor, exists := mediaActor(argRequest)
	if !exists {
		writeMediaContextError(argResponse)

		return
	}

	grants, err := handler.grants.GrantsByMedia(
		argRequest.Context(),
		actor,
		argRequest.PathValue("id"),
	)
	if err != nil {
		writeMediaGrantApplicationError(argResponse, err)

		return
	}

	responses := make([]mediaGrantResponse, 0, len(grants))
	for _, grant := range grants {
		responses = append(responses, newMediaGrantResponse(grant))
	}
	argResponse.Header().Set("Content-Type", "application/json; charset=utf-8")
	argResponse.Header().Set("Cache-Control", "no-store")
	argResponse.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(argResponse).Encode(struct {
		Grants []mediaGrantResponse `json:"grants"`
	}{Grants: responses})
}

func (handler *mediaGrantHandler) revoke(
	argResponse http.ResponseWriter,
	argRequest *http.Request,
) {
	actor, exists := mediaActor(argRequest)
	if !exists {
		writeMediaContextError(argResponse)

		return
	}

	if err := handler.grants.RevokeGrant(
		argRequest.Context(),
		actor,
		argRequest.PathValue("id"),
		argRequest.PathValue("userId"),
	); err != nil {
		writeMediaGrantApplicationError(argResponse, err)

		return
	}

	argResponse.Header().Set("Cache-Control", "no-store")
	argResponse.WriteHeader(http.StatusNoContent)
}

func decodePermissionSet(
	argResponse http.ResponseWriter,
	argRequest *http.Request,
) (domainmedia.PermissionSet, bool) {
	var requestBody mediaGrantRequest
	if err := decodeJSONRequest(argResponse, argRequest, &requestBody); err != nil {
		writeInvalidRequest(argResponse)

		return 0, false
	}

	permissions := make([]domainmedia.Permission, 0, len(requestBody.Permissions))
	for _, name := range requestBody.Permissions {
		permission, err := domainmedia.ParsePermission(name)
		if err != nil {
			writeInvalidRequest(argResponse)

			return 0, false
		}
		permissions = append(permissions, permission)
	}
	permissionSet, err := domainmedia.NewPermissionSet(permissions...)
	if err != nil {
		writeInvalidRequest(argResponse)

		return 0, false
	}

	return permissionSet, true
}

func writeMediaGrantResponse(argResponse http.ResponseWriter, argGrant domainmedia.Grant) {
	argResponse.Header().Set("Content-Type", "application/json; charset=utf-8")
	argResponse.Header().Set("Cache-Control", "no-store")
	argResponse.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(argResponse).Encode(newMediaGrantResponse(argGrant))
}

func newMediaGrantResponse(argGrant domainmedia.Grant) mediaGrantResponse {
	values := argGrant.Permissions.Values()
	permissions := make([]string, 0, len(values))
	for _, permission := range values {
		permissions = append(permissions, permission.String())
	}

	return mediaGrantResponse{
		MediaID:     argGrant.MediaID,
		UserID:      argGrant.UserID,
		Permissions: permissions,
	}
}

func writeMediaGrantApplicationError(argResponse http.ResponseWriter, argError error) {
	switch {
	case isInvalidGrantInput(argError):
		writeInvalidRequest(argResponse)
	case errors.Is(argError, appmedia.ErrMediaNotFound),
		errors.Is(argError, domainmedia.ErrGrantNotFound),
		errors.Is(argError, identity.ErrUserNotFound):
		writeJSONError(argResponse, http.StatusNotFound, "not_found", "Resource not found.")
	case errors.Is(argError, appmedia.ErrOwnerGrant):
		writeJSONError(
			argResponse,
			http.StatusConflict,
			"owner_grant",
			"A media owner cannot receive an explicit grant.",
		)
	case errors.Is(argError, appmedia.ErrInactiveGrantee):
		writeJSONError(
			argResponse,
			http.StatusConflict,
			"inactive_grantee",
			"An inactive user cannot receive a media grant.",
		)
	default:
		writeMediaContextError(argResponse)
	}
}

func isInvalidGrantInput(argError error) bool {
	return errors.Is(argError, identity.ErrInvalidUserID) ||
		errors.Is(argError, domainmedia.ErrInvalidPermission) ||
		errors.Is(argError, domainmedia.ErrInvalidPermissionSet) ||
		errors.Is(argError, domainmedia.ErrInvalidGrant)
}
