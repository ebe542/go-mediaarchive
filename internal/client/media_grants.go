package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

// MediaGrant is a validated public API media permission grant.
type MediaGrant struct {
	MediaID     string
	UserID      string
	Permissions domainmedia.PermissionSet
}

type mediaGrantRequest struct {
	Permissions []string `json:"permissions"`
}

type mediaGrantResponse struct {
	MediaID     string   `json:"mediaId"`
	UserID      string   `json:"userId"`
	Permissions []string `json:"permissions"`
}

// ReplaceMediaGrant replaces one user's complete permission set for a medium.
func (client *Client) ReplaceMediaGrant(
	argContext context.Context,
	argAccessToken string,
	argMediaID string,
	argUserID string,
	argPermissions domainmedia.PermissionSet,
) (MediaGrant, error) {
	request, err := newMediaGrantRequest(argPermissions)
	if err != nil {
		return MediaGrant{}, err
	}
	var response mediaGrantResponse
	if err := client.doJSON(
		argContext,
		http.MethodPut,
		mediaGrantPath(argMediaID, argUserID),
		argAccessToken,
		request,
		http.StatusOK,
		&response,
		"replace media grant",
	); err != nil {
		return MediaGrant{}, err
	}

	return decodeMediaGrantResponse(response, "replace media grant")
}

// MediaGrantByUser returns one authorized per-user media grant.
func (client *Client) MediaGrantByUser(
	argContext context.Context,
	argAccessToken string,
	argMediaID string,
	argUserID string,
) (MediaGrant, error) {
	var response mediaGrantResponse
	if err := client.doJSON(
		argContext,
		http.MethodGet,
		mediaGrantPath(argMediaID, argUserID),
		argAccessToken,
		nil,
		http.StatusOK,
		&response,
		"media grant by user",
	); err != nil {
		return MediaGrant{}, err
	}

	return decodeMediaGrantResponse(response, "media grant by user")
}

// ListMediaGrants returns every explicit grant for one authorized medium.
func (client *Client) ListMediaGrants(
	argContext context.Context,
	argAccessToken string,
	argMediaID string,
) ([]MediaGrant, error) {
	var response struct {
		Grants []mediaGrantResponse `json:"grants"`
	}
	if err := client.doJSON(
		argContext,
		http.MethodGet,
		"/api/v1/media/"+url.PathEscape(argMediaID)+"/grants",
		argAccessToken,
		nil,
		http.StatusOK,
		&response,
		"list media grants",
	); err != nil {
		return nil, err
	}
	if response.Grants == nil {
		return nil, errors.New("validate media grant list response: grants are required")
	}

	grants := make([]MediaGrant, 0, len(response.Grants))
	for _, grantResponse := range response.Grants {
		grant, err := decodeMediaGrantResponse(grantResponse, "list media grants")
		if err != nil {
			return nil, err
		}
		grants = append(grants, grant)
	}

	return grants, nil
}

// RevokeMediaGrant permanently removes one explicit media grant.
func (client *Client) RevokeMediaGrant(
	argContext context.Context,
	argAccessToken string,
	argMediaID string,
	argUserID string,
) error {
	return client.doJSON(
		argContext,
		http.MethodDelete,
		mediaGrantPath(argMediaID, argUserID),
		argAccessToken,
		nil,
		http.StatusNoContent,
		nil,
		"revoke media grant",
	)
}

func newMediaGrantRequest(
	argPermissions domainmedia.PermissionSet,
) (mediaGrantRequest, error) {
	if !argPermissions.Valid() {
		return mediaGrantRequest{}, fmt.Errorf(
			"validate media grant request: %w",
			domainmedia.ErrInvalidPermissionSet,
		)
	}

	return mediaGrantRequest{Permissions: permissionNames(argPermissions)}, nil
}

func decodeMediaGrantResponse(
	argResponse mediaGrantResponse,
	argOperation string,
) (MediaGrant, error) {
	if argResponse.Permissions == nil {
		return MediaGrant{}, fmt.Errorf(
			"validate %s response: permissions are required",
			argOperation,
		)
	}
	permissions := make([]domainmedia.Permission, 0, len(argResponse.Permissions))
	for _, name := range argResponse.Permissions {
		permission, err := domainmedia.ParsePermission(name)
		if err != nil {
			return MediaGrant{}, fmt.Errorf(
				"validate %s response permission: %w",
				argOperation,
				err,
			)
		}
		permissions = append(permissions, permission)
	}
	permissionSet, err := domainmedia.NewPermissionSet(permissions...)
	if err != nil {
		return MediaGrant{}, fmt.Errorf("validate %s response: %w", argOperation, err)
	}
	grant, err := domainmedia.NewGrant(argResponse.MediaID, argResponse.UserID, permissionSet)
	if err != nil {
		return MediaGrant{}, fmt.Errorf("validate %s response: %w", argOperation, err)
	}

	return MediaGrant{
		MediaID:     grant.MediaID,
		UserID:      grant.UserID,
		Permissions: grant.Permissions,
	}, nil
}

func mediaGrantPath(argMediaID string, argUserID string) string {
	return "/api/v1/media/" + url.PathEscape(argMediaID) +
		"/grants/" + url.PathEscape(argUserID)
}

func permissionNames(argPermissions domainmedia.PermissionSet) []string {
	values := argPermissions.Values()
	names := make([]string, 0, len(values))
	for _, permission := range values {
		names = append(names, permission.String())
	}

	return names
}
