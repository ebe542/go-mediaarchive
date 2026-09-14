package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	appmedia "github.com/ebe542/go-mediaarchive/internal/application/media"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

// MediaMetadataService performs authorized media metadata operations.
type MediaMetadataService interface {
	CreateItem(
		context.Context,
		identity.User,
		appmedia.CreateItemInput,
	) (domainmedia.Item, error)
	ItemByID(context.Context, identity.User, string) (domainmedia.Item, error)
	UpdateItem(
		context.Context,
		identity.User,
		string,
		appmedia.UpdateItemInput,
	) (domainmedia.Item, error)
	DeleteItem(context.Context, identity.User, string) error
}

// WithMediaMetadataAPI enables authenticated media metadata endpoints.
func WithMediaMetadataAPI(
	argResolver SessionResolver,
	argService MediaMetadataService,
) Option {
	return func(argConfiguration *handlerConfiguration) {
		argConfiguration.mediaResolver = argResolver
		argConfiguration.mediaMetadata = argService
	}
}

type mediaMetadataHandler struct {
	media MediaMetadataService
}

type mediaMetadataRequest struct {
	Title            string           `json:"title"`
	Authors          []string         `json:"authors"`
	OriginalFilename string           `json:"originalFilename"`
	Type             domainmedia.Type `json:"type"`
	MIMEType         string           `json:"mimeType"`
	Size             int64            `json:"size"`
	SHA256           string           `json:"sha256"`
}

type mediaMetadataResponse struct {
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

func (handler *mediaMetadataHandler) create(
	argResponse http.ResponseWriter,
	argRequest *http.Request,
) {
	actor, exists := mediaActor(argRequest)
	if !exists {
		writeMediaContextError(argResponse)

		return
	}

	input, ok := decodeMediaMetadataInput(argResponse, argRequest)
	if !ok {
		return
	}
	item, err := handler.media.CreateItem(argRequest.Context(), actor, input)
	if err != nil {
		writeMediaApplicationError(argResponse, err)

		return
	}

	argResponse.Header().Set("Location", "/api/v1/media/"+item.ID)
	writeMediaMetadataResponse(argResponse, item, http.StatusCreated)
}

func (handler *mediaMetadataHandler) read(
	argResponse http.ResponseWriter,
	argRequest *http.Request,
) {
	actor, exists := mediaActor(argRequest)
	if !exists {
		writeMediaContextError(argResponse)

		return
	}

	item, err := handler.media.ItemByID(
		argRequest.Context(),
		actor,
		argRequest.PathValue("id"),
	)
	if err != nil {
		writeMediaApplicationError(argResponse, err)

		return
	}

	writeMediaMetadataResponse(argResponse, item, http.StatusOK)
}

func (handler *mediaMetadataHandler) update(
	argResponse http.ResponseWriter,
	argRequest *http.Request,
) {
	actor, exists := mediaActor(argRequest)
	if !exists {
		writeMediaContextError(argResponse)

		return
	}

	input, ok := decodeMediaMetadataInput(argResponse, argRequest)
	if !ok {
		return
	}
	item, err := handler.media.UpdateItem(
		argRequest.Context(),
		actor,
		argRequest.PathValue("id"),
		input,
	)
	if err != nil {
		writeMediaApplicationError(argResponse, err)

		return
	}

	writeMediaMetadataResponse(argResponse, item, http.StatusOK)
}

func (handler *mediaMetadataHandler) delete(
	argResponse http.ResponseWriter,
	argRequest *http.Request,
) {
	actor, exists := mediaActor(argRequest)
	if !exists {
		writeMediaContextError(argResponse)

		return
	}

	if err := handler.media.DeleteItem(
		argRequest.Context(),
		actor,
		argRequest.PathValue("id"),
	); err != nil {
		writeMediaApplicationError(argResponse, err)

		return
	}

	argResponse.Header().Set("Cache-Control", "no-store")
	argResponse.WriteHeader(http.StatusNoContent)
}

func decodeMediaMetadataInput(
	argResponse http.ResponseWriter,
	argRequest *http.Request,
) (appmedia.CreateItemInput, bool) {
	var requestBody mediaMetadataRequest
	if err := decodeJSONRequest(argResponse, argRequest, &requestBody); err != nil {
		writeInvalidRequest(argResponse)

		return appmedia.CreateItemInput{}, false
	}
	if len(requestBody.SHA256) != sha256.Size*2 ||
		requestBody.SHA256 != strings.ToLower(requestBody.SHA256) {
		writeInvalidRequest(argResponse)

		return appmedia.CreateItemInput{}, false
	}
	checksum, err := hex.DecodeString(requestBody.SHA256)
	if err != nil {
		writeInvalidRequest(argResponse)

		return appmedia.CreateItemInput{}, false
	}

	return appmedia.CreateItemInput{
		Title:            requestBody.Title,
		Authors:          requestBody.Authors,
		OriginalFilename: requestBody.OriginalFilename,
		Type:             requestBody.Type,
		MIMEType:         requestBody.MIMEType,
		Size:             requestBody.Size,
		Checksum:         checksum,
	}, true
}

func mediaActor(argRequest *http.Request) (identity.User, bool) {
	return AuthenticatedUser(argRequest.Context())
}

func writeMediaMetadataResponse(
	argResponse http.ResponseWriter,
	argItem domainmedia.Item,
	argStatus int,
) {
	authors := append([]string{}, argItem.Authors...)
	argResponse.Header().Set("Content-Type", "application/json; charset=utf-8")
	argResponse.Header().Set("Cache-Control", "no-store")
	argResponse.WriteHeader(argStatus)

	_ = json.NewEncoder(argResponse).Encode(mediaMetadataResponse{
		ID:               argItem.ID,
		Title:            argItem.Title,
		Authors:          authors,
		OriginalFilename: argItem.OriginalFilename,
		Type:             argItem.Type,
		MIMEType:         argItem.MIMEType,
		Size:             argItem.Size,
		SHA256:           hex.EncodeToString(argItem.Checksum[:]),
		OwnerID:          argItem.OwnerID,
		CreatedAt:        argItem.CreatedAt.UTC(),
		UpdatedAt:        argItem.UpdatedAt.UTC(),
	})
}

func writeMediaContextError(argResponse http.ResponseWriter) {
	writeJSONError(
		argResponse,
		http.StatusInternalServerError,
		"internal_error",
		"Internal server error.",
	)
}

func writeMediaApplicationError(argResponse http.ResponseWriter, argError error) {
	switch {
	case isInvalidMediaInput(argError):
		writeInvalidRequest(argResponse)
	case errors.Is(argError, appmedia.ErrCreationForbidden):
		writeJSONError(argResponse, http.StatusForbidden, "forbidden", "Access forbidden.")
	case errors.Is(argError, appmedia.ErrMediaNotFound):
		writeJSONError(argResponse, http.StatusNotFound, "not_found", "Resource not found.")
	default:
		writeMediaContextError(argResponse)
	}
}

func isInvalidMediaInput(argError error) bool {
	return errors.Is(argError, domainmedia.ErrInvalidTitle) ||
		errors.Is(argError, domainmedia.ErrInvalidAuthors) ||
		errors.Is(argError, domainmedia.ErrInvalidOriginalFilename) ||
		errors.Is(argError, domainmedia.ErrInvalidMediaType) ||
		errors.Is(argError, domainmedia.ErrInvalidMIMEType) ||
		errors.Is(argError, domainmedia.ErrInvalidSize) ||
		errors.Is(argError, domainmedia.ErrInvalidChecksum)
}
