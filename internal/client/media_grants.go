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
	ctx context.Context,
	accessToken string,
	mediaID string,
	userID string,
	permissions domainmedia.PermissionSet,
) (MediaGrant, error) {
	request, err := newMediaGrantRequest(permissions)
	if err != nil {
		return MediaGrant{}, err
	}
	var response mediaGrantResponse
	if err := client.doJSON(
		ctx,
		http.MethodPut,
		mediaGrantPath(mediaID, userID),
		accessToken,
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
	ctx context.Context,
	accessToken string,
	mediaID string,
	userID string,
) (MediaGrant, error) {
	var response mediaGrantResponse
	if err := client.doJSON(
		ctx,
		http.MethodGet,
		mediaGrantPath(mediaID, userID),
		accessToken,
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
	ctx context.Context,
	accessToken string,
	mediaID string,
) ([]MediaGrant, error) {
	var response struct {
		Grants []mediaGrantResponse `json:"grants"`
	}
	if err := client.doJSON(
		ctx,
		http.MethodGet,
		"/api/v1/media/"+url.PathEscape(mediaID)+"/grants",
		accessToken,
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
	ctx context.Context,
	accessToken string,
	mediaID string,
	userID string,
) error {
	return client.doJSON(
		ctx,
		http.MethodDelete,
		mediaGrantPath(mediaID, userID),
		accessToken,
		nil,
		http.StatusNoContent,
		nil,
		"revoke media grant",
	)
}

func newMediaGrantRequest(
	permissions domainmedia.PermissionSet,
) (mediaGrantRequest, error) {
	if !permissions.Valid() {
		return mediaGrantRequest{}, fmt.Errorf(
			"validate media grant request: %w",
			domainmedia.ErrInvalidPermissionSet,
		)
	}

	return mediaGrantRequest{Permissions: permissionNames(permissions)}, nil
}

func decodeMediaGrantResponse(
	response mediaGrantResponse,
	operation string,
) (MediaGrant, error) {
	if response.Permissions == nil {
		return MediaGrant{}, fmt.Errorf(
			"validate %s response: permissions are required",
			operation,
		)
	}
	permissions := make([]domainmedia.Permission, 0, len(response.Permissions))
	for _, name := range response.Permissions {
		permission, err := domainmedia.ParsePermission(name)
		if err != nil {
			return MediaGrant{}, fmt.Errorf(
				"validate %s response permission: %w",
				operation,
				err,
			)
		}
		permissions = append(permissions, permission)
	}
	permissionSet, err := domainmedia.NewPermissionSet(permissions...)
	if err != nil {
		return MediaGrant{}, fmt.Errorf("validate %s response: %w", operation, err)
	}
	grant, err := domainmedia.NewGrant(response.MediaID, response.UserID, permissionSet)
	if err != nil {
		return MediaGrant{}, fmt.Errorf("validate %s response: %w", operation, err)
	}

	return MediaGrant{
		MediaID:     grant.MediaID,
		UserID:      grant.UserID,
		Permissions: grant.Permissions,
	}, nil
}

func mediaGrantPath(mediaID string, userID string) string {
	return "/api/v1/media/" + url.PathEscape(mediaID) +
		"/grants/" + url.PathEscape(userID)
}

func permissionNames(permissions domainmedia.PermissionSet) []string {
	values := permissions.Values()
	names := make([]string, 0, len(values))
	for _, permission := range values {
		names = append(names, permission.String())
	}

	return names
}
