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
	ctx context.Context,
	accessToken string,
	input MediaInput,
) (Media, error) {
	return client.mutateMedia(
		ctx,
		http.MethodPost,
		"/api/v1/media",
		accessToken,
		input,
		http.StatusCreated,
		"create media",
	)
}

// MediaByID returns discoverable metadata for one media identity.
func (client *Client) MediaByID(
	ctx context.Context,
	accessToken string,
	id string,
) (Media, error) {
	var response mediaResponse
	if err := client.doJSON(
		ctx,
		http.MethodGet,
		"/api/v1/media/"+url.PathEscape(id),
		accessToken,
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
	ctx context.Context,
	accessToken string,
	id string,
	input MediaInput,
) (Media, error) {
	return client.mutateMedia(
		ctx,
		http.MethodPut,
		"/api/v1/media/"+url.PathEscape(id),
		accessToken,
		input,
		http.StatusOK,
		"update media",
	)
}

// DeleteMedia permanently removes one authorized media identity.
func (client *Client) DeleteMedia(
	ctx context.Context,
	accessToken string,
	id string,
) error {
	return client.doJSON(
		ctx,
		http.MethodDelete,
		"/api/v1/media/"+url.PathEscape(id),
		accessToken,
		nil,
		http.StatusNoContent,
		nil,
		"delete media",
	)
}

func (client *Client) mutateMedia(
	ctx context.Context,
	method string,
	path string,
	accessToken string,
	input MediaInput,
	expectedStatus int,
	operation string,
) (Media, error) {
	request := mediaRequest{
		Title:            input.Title,
		Authors:          append([]string{}, input.Authors...),
		OriginalFilename: input.OriginalFilename,
		Type:             input.Type,
		MIMEType:         input.MIMEType,
		Size:             input.Size,
		SHA256:           hex.EncodeToString(input.Checksum[:]),
	}
	var response mediaResponse
	if err := client.doJSON(
		ctx,
		method,
		path,
		accessToken,
		request,
		expectedStatus,
		&response,
		operation,
	); err != nil {
		return Media{}, err
	}

	return decodeMediaResponse(response, operation)
}

func decodeMediaResponse(response mediaResponse, operation string) (Media, error) {
	if response.Authors == nil ||
		len(response.SHA256) != sha256.Size*2 ||
		response.SHA256 != strings.ToLower(response.SHA256) {
		return Media{}, fmt.Errorf("validate %s response: media is incomplete", operation)
	}
	checksum, err := hex.DecodeString(response.SHA256)
	if err != nil {
		return Media{}, fmt.Errorf("validate %s response checksum: %w", operation, err)
	}
	item, err := domainmedia.NewItem(
		response.ID,
		response.Title,
		response.Authors,
		response.OriginalFilename,
		response.Type,
		response.MIMEType,
		response.Size,
		checksum,
		response.OwnerID,
		response.CreatedAt,
		response.UpdatedAt,
	)
	if err != nil {
		return Media{}, fmt.Errorf("validate %s response: %w", operation, err)
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
