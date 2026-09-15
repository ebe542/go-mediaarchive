package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

// MediaInput contains caller-controlled media metadata.
type MediaInput struct {
	Title            string
	Authors          []string
	OriginalFilename string
	Type             domainmedia.Type
	MIMEType         string
	Size             int64
	Checksum         [sha256.Size]byte
}

// Media is the validated public API representation of a media identity.
type Media struct {
	ID               string
	Title            string
	Authors          []string
	OriginalFilename string
	Type             domainmedia.Type
	MIMEType         string
	Size             int64
	Checksum         [sha256.Size]byte
	OwnerID          string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type mediaRequest struct {
	Title            string           `json:"title"`
	Authors          []string         `json:"authors"`
	OriginalFilename string           `json:"originalFilename"`
	Type             domainmedia.Type `json:"type"`
	MIMEType         string           `json:"mimeType"`
	Size             int64            `json:"size"`
	SHA256           string           `json:"sha256"`
}

type mediaResponse struct {
	ID               string           `json:"id"`
	Title            string           `json:"title"`
	Authors          []string         `json:"authors"`
	OriginalFilename string           `json:"originalFilename"`
	Type             domainmedia.Type `json:"type"`
	MIMEType         string           `json:"mimeType"`
	Size             int64            `json:"size"`
	SHA256           string           `json:"sha256"`
	OwnerID          string           `json:"ownerId"`
	CreatedAt        time.Time        `json:"createdAt"`
	UpdatedAt        time.Time        `json:"updatedAt"`
}

// CreateMedia creates a media identity owned by the authenticated user.
func (client *Client) CreateMedia(
	argContext context.Context,
	argAccessToken string,
	argInput MediaInput,
) (Media, error) {
	return client.mutateMedia(
		argContext,
		http.MethodPost,
		"/api/v1/media",
		argAccessToken,
		argInput,
		http.StatusCreated,
		"create media",
	)
}

// MediaByID returns discoverable metadata for one media identity.
func (client *Client) MediaByID(
	argContext context.Context,
	argAccessToken string,
	argID string,
) (Media, error) {
	var response mediaResponse
	if err := client.doJSON(
		argContext,
		http.MethodGet,
		"/api/v1/media/"+url.PathEscape(argID),
		argAccessToken,
		nil,
		http.StatusOK,
		&response,
		"media by ID",
	); err != nil {
		return Media{}, err
	}

	return decodeMediaResponse(response, "media by ID")
}

// UpdateMedia replaces mutable metadata for one media identity.
func (client *Client) UpdateMedia(
	argContext context.Context,
	argAccessToken string,
	argID string,
	argInput MediaInput,
) (Media, error) {
	return client.mutateMedia(
		argContext,
		http.MethodPut,
		"/api/v1/media/"+url.PathEscape(argID),
		argAccessToken,
		argInput,
		http.StatusOK,
		"update media",
	)
}

// DeleteMedia permanently removes one authorized media identity.
func (client *Client) DeleteMedia(
	argContext context.Context,
	argAccessToken string,
	argID string,
) error {
	return client.doJSON(
		argContext,
		http.MethodDelete,
		"/api/v1/media/"+url.PathEscape(argID),
		argAccessToken,
		nil,
		http.StatusNoContent,
		nil,
		"delete media",
	)
}

func (client *Client) mutateMedia(
	argContext context.Context,
	argMethod string,
	argPath string,
	argAccessToken string,
	argInput MediaInput,
	argExpectedStatus int,
	argOperation string,
) (Media, error) {
	request := mediaRequest{
		Title:            argInput.Title,
		Authors:          append([]string{}, argInput.Authors...),
		OriginalFilename: argInput.OriginalFilename,
		Type:             argInput.Type,
		MIMEType:         argInput.MIMEType,
		Size:             argInput.Size,
		SHA256:           hex.EncodeToString(argInput.Checksum[:]),
	}
	var response mediaResponse
	if err := client.doJSON(
		argContext,
		argMethod,
		argPath,
		argAccessToken,
		request,
		argExpectedStatus,
		&response,
		argOperation,
	); err != nil {
		return Media{}, err
	}

	return decodeMediaResponse(response, argOperation)
}

func decodeMediaResponse(argResponse mediaResponse, argOperation string) (Media, error) {
	if argResponse.Authors == nil ||
		len(argResponse.SHA256) != sha256.Size*2 ||
		argResponse.SHA256 != strings.ToLower(argResponse.SHA256) {
		return Media{}, fmt.Errorf("validate %s response: media is incomplete", argOperation)
	}
	checksum, err := hex.DecodeString(argResponse.SHA256)
	if err != nil {
		return Media{}, fmt.Errorf("validate %s response checksum: %w", argOperation, err)
	}
	item, err := domainmedia.NewItem(
		argResponse.ID,
		argResponse.Title,
		argResponse.Authors,
		argResponse.OriginalFilename,
		argResponse.Type,
		argResponse.MIMEType,
		argResponse.Size,
		checksum,
		argResponse.OwnerID,
		argResponse.CreatedAt,
		argResponse.UpdatedAt,
	)
	if err != nil {
		return Media{}, fmt.Errorf("validate %s response: %w", argOperation, err)
	}

	return Media{
		ID:               item.ID,
		Title:            item.Title,
		Authors:          item.Authors,
		OriginalFilename: item.OriginalFilename,
		Type:             item.Type,
		MIMEType:         item.MIMEType,
		Size:             item.Size,
		Checksum:         item.Checksum,
		OwnerID:          item.OwnerID,
		CreatedAt:        item.CreatedAt,
		UpdatedAt:        item.UpdatedAt,
	}, nil
}
