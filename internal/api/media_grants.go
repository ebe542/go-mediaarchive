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
	resolver SessionResolver,
	service MediaGrantService,
) Option {
	return func(configuration *handlerConfiguration) {
		configuration.mediaGrantResolver = resolver
		configuration.mediaGrants = service
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
	response http.ResponseWriter,
	request *http.Request,
) {
	actor, exists := mediaActor(request)
	if !exists {
		writeMediaContextError(response)

		return
	}

	permissions, ok := decodePermissionSet(response, request)
	if !ok {
		return
	}
	grant, err := handler.grants.ReplaceGrant(
		request.Context(),
		actor,
		request.PathValue("id"),
		request.PathValue("userId"),
		permissions,
	)
	if err != nil {
		writeMediaGrantApplicationError(response, err)

		return
	}

	writeMediaGrantResponse(response, grant)
}

func (handler *mediaGrantHandler) read(
	response http.ResponseWriter,
	request *http.Request,
) {
	actor, exists := mediaActor(request)
	if !exists {
		writeMediaContextError(response)

		return
	}

	grant, err := handler.grants.GrantByUser(
		request.Context(),
		actor,
		request.PathValue("id"),
		request.PathValue("userId"),
	)
	if err != nil {
		writeMediaGrantApplicationError(response, err)

		return
	}

	writeMediaGrantResponse(response, grant)
}

func (handler *mediaGrantHandler) list(
	response http.ResponseWriter,
	request *http.Request,
) {
	actor, exists := mediaActor(request)
	if !exists {
		writeMediaContextError(response)

		return
	}

	grants, err := handler.grants.GrantsByMedia(
		request.Context(),
		actor,
		request.PathValue("id"),
	)
	if err != nil {
		writeMediaGrantApplicationError(response, err)

		return
	}

	responses := make([]mediaGrantResponse, 0, len(grants))
	for _, grant := range grants {
		responses = append(responses, newMediaGrantResponse(grant))
	}
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(response).Encode(struct {
		Grants []mediaGrantResponse `json:"grants"`
	}{Grants: responses})
}

func (handler *mediaGrantHandler) revoke(
	response http.ResponseWriter,
	request *http.Request,
) {
	actor, exists := mediaActor(request)
	if !exists {
		writeMediaContextError(response)

		return
	}

	if err := handler.grants.RevokeGrant(
		request.Context(),
		actor,
		request.PathValue("id"),
		request.PathValue("userId"),
	); err != nil {
		writeMediaGrantApplicationError(response, err)

		return
	}

	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func decodePermissionSet(
	response http.ResponseWriter,
	request *http.Request,
) (domainmedia.PermissionSet, bool) {
	var requestBody mediaGrantRequest
	if err := decodeJSONRequest(response, request, &requestBody); err != nil {
		writeInvalidRequest(response)

		return 0, false
	}

	permissions := make([]domainmedia.Permission, 0, len(requestBody.Permissions))
	for _, name := range requestBody.Permissions {
		permission, err := domainmedia.ParsePermission(name)
		if err != nil {
			writeInvalidRequest(response)

			return 0, false
		}
		permissions = append(permissions, permission)
	}
	permissionSet, err := domainmedia.NewPermissionSet(permissions...)
	if err != nil {
		writeInvalidRequest(response)

		return 0, false
	}

	return permissionSet, true
}

func writeMediaGrantResponse(response http.ResponseWriter, grant domainmedia.Grant) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(response).Encode(newMediaGrantResponse(grant))
}

func newMediaGrantResponse(grant domainmedia.Grant) mediaGrantResponse {
	values := grant.Permissions.Values()
	permissions := make([]string, 0, len(values))
	for _, permission := range values {
		permissions = append(permissions, permission.String())
	}

	return mediaGrantResponse{
		MediaID:     grant.MediaID,
		UserID:      grant.UserID,
		Permissions: permissions,
	}
}

func writeMediaGrantApplicationError(response http.ResponseWriter, inputError error) {
	switch {
	case isInvalidGrantInput(inputError):
		writeInvalidRequest(response)
	case errors.Is(inputError, appmedia.ErrMediaNotFound),
		errors.Is(inputError, domainmedia.ErrGrantNotFound),
		errors.Is(inputError, identity.ErrUserNotFound):
		writeJSONError(response, http.StatusNotFound, "not_found", "Resource not found.")
	case errors.Is(inputError, appmedia.ErrOwnerGrant):
		writeJSONError(
			response,
			http.StatusConflict,
			"owner_grant",
			"A media owner cannot receive an explicit grant.",
		)
	case errors.Is(inputError, appmedia.ErrInactiveGrantee):
		writeJSONError(
			response,
			http.StatusConflict,
			"inactive_grantee",
			"An inactive user cannot receive a media grant.",
		)
	default:
		writeMediaContextError(response)
	}
}

func isInvalidGrantInput(inputError error) bool {
	return errors.Is(inputError, identity.ErrInvalidUserID) ||
		errors.Is(inputError, domainmedia.ErrInvalidPermission) ||
		errors.Is(inputError, domainmedia.ErrInvalidPermissionSet) ||
		errors.Is(inputError, domainmedia.ErrInvalidGrant)
}
