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
	resolver SessionResolver,
	service MediaMetadataService,
) Option {
	return func(configuration *handlerConfiguration) {
		configuration.mediaResolver = resolver
		configuration.mediaMetadata = service
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
	response http.ResponseWriter,
	request *http.Request,
) {
	actor, exists := mediaActor(request)
	if !exists {
		writeMediaContextError(response)

		return
	}

	input, ok := decodeMediaMetadataInput(response, request)
	if !ok {
		return
	}
	item, err := handler.media.CreateItem(request.Context(), actor, input)
	if err != nil {
		writeMediaApplicationError(response, err)

		return
	}

	response.Header().Set("Location", "/api/v1/media/"+item.ID)
	writeMediaMetadataResponse(response, item, http.StatusCreated)
}

func (handler *mediaMetadataHandler) read(
	response http.ResponseWriter,
	request *http.Request,
) {
	actor, exists := mediaActor(request)
	if !exists {
		writeMediaContextError(response)

		return
	}

	item, err := handler.media.ItemByID(
		request.Context(),
		actor,
		request.PathValue("id"),
	)
	if err != nil {
		writeMediaApplicationError(response, err)

		return
	}

	writeMediaMetadataResponse(response, item, http.StatusOK)
}

func (handler *mediaMetadataHandler) update(
	response http.ResponseWriter,
	request *http.Request,
) {
	actor, exists := mediaActor(request)
	if !exists {
		writeMediaContextError(response)

		return
	}

	input, ok := decodeMediaMetadataInput(response, request)
	if !ok {
		return
	}
	item, err := handler.media.UpdateItem(
		request.Context(),
		actor,
		request.PathValue("id"),
		input,
	)
	if err != nil {
		writeMediaApplicationError(response, err)

		return
	}

	writeMediaMetadataResponse(response, item, http.StatusOK)
}

func (handler *mediaMetadataHandler) delete(
	response http.ResponseWriter,
	request *http.Request,
) {
	actor, exists := mediaActor(request)
	if !exists {
		writeMediaContextError(response)

		return
	}

	if err := handler.media.DeleteItem(
		request.Context(),
		actor,
		request.PathValue("id"),
	); err != nil {
		writeMediaApplicationError(response, err)

		return
	}

	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func decodeMediaMetadataInput(
	response http.ResponseWriter,
	request *http.Request,
) (appmedia.CreateItemInput, bool) {
	var requestBody mediaMetadataRequest
	if err := decodeJSONRequest(response, request, &requestBody); err != nil {
		writeInvalidRequest(response)

		return appmedia.CreateItemInput{}, false
	}
	if len(requestBody.SHA256) != sha256.Size*2 ||
		requestBody.SHA256 != strings.ToLower(requestBody.SHA256) {
		writeInvalidRequest(response)

		return appmedia.CreateItemInput{}, false
	}
	checksum, err := hex.DecodeString(requestBody.SHA256)
	if err != nil {
		writeInvalidRequest(response)

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

func mediaActor(request *http.Request) (identity.User, bool) {
	return AuthenticatedUser(request.Context())
}

func writeMediaMetadataResponse(
	response http.ResponseWriter,
	item domainmedia.Item,
	status int,
) {
	authors := append([]string{}, item.Authors...)
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)

	_ = json.NewEncoder(response).Encode(mediaMetadataResponse{
		ID:               item.ID,
		Title:            item.Title,
		Authors:          authors,
		OriginalFilename: item.OriginalFilename,
		Type:             item.Type,
		MIMEType:         item.MIMEType,
		Size:             item.Size,
		SHA256:           hex.EncodeToString(item.Checksum[:]),
		OwnerID:          item.OwnerID,
		CreatedAt:        item.CreatedAt.UTC(),
		UpdatedAt:        item.UpdatedAt.UTC(),
	})
}

func writeMediaContextError(response http.ResponseWriter) {
	writeJSONError(
		response,
		http.StatusInternalServerError,
		"internal_error",
		"Internal server error.",
	)
}

func writeMediaApplicationError(response http.ResponseWriter, inputError error) {
	switch {
	case isInvalidMediaInput(inputError):
		writeInvalidRequest(response)
	case errors.Is(inputError, appmedia.ErrCreationForbidden):
		writeJSONError(response, http.StatusForbidden, "forbidden", "Access forbidden.")
	case errors.Is(inputError, appmedia.ErrMediaNotFound):
		writeJSONError(response, http.StatusNotFound, "not_found", "Resource not found.")
	case errors.Is(inputError, domainmedia.ErrItemConflict):
		writeJSONError(response, http.StatusConflict, "conflict", "Resource conflict.")
	default:
		writeMediaContextError(response)
	}
}

func isInvalidMediaInput(inputError error) bool {
	return errors.Is(inputError, domainmedia.ErrInvalidTitle) ||
		errors.Is(inputError, domainmedia.ErrInvalidAuthors) ||
		errors.Is(inputError, domainmedia.ErrInvalidOriginalFilename) ||
		errors.Is(inputError, domainmedia.ErrInvalidMediaType) ||
		errors.Is(inputError, domainmedia.ErrInvalidMIMEType) ||
		errors.Is(inputError, domainmedia.ErrInvalidSize) ||
		errors.Is(inputError, domainmedia.ErrInvalidChecksum)
}
