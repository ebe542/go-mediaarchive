package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"

	appmedia "github.com/ebe542/go-mediaarchive/internal/application/media"
	"github.com/ebe542/go-mediaarchive/internal/content"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

const maximumMultipartOverhead = 256 * 1024

// MediaUploadService stores authenticated managed media uploads.
type MediaUploadService interface {
	UploadItem(
		context.Context,
		identity.User,
		appmedia.UploadInput,
	) (domainmedia.Item, error)
}

// WithMediaUploadAPI enables authenticated streaming media uploads.
func WithMediaUploadAPI(
	resolver SessionResolver,
	service MediaUploadService,
	maximumSize int64,
) Option {
	return func(configuration *handlerConfiguration) {
		configuration.mediaUploadResolver = resolver
		configuration.mediaUploads = service
		configuration.maximumUploadSize = maximumSize
	}
}

type mediaUploadHandler struct {
	uploads     MediaUploadService
	maximumSize int64
}

type mediaUploadMetadata struct {
	Title   string           `json:"title"`
	Authors []string         `json:"authors"`
	Type    domainmedia.Type `json:"type"`
}

type finalMultipartFile struct {
	part      *multipart.Part
	reader    *multipart.Reader
	validated bool
}

func (file *finalMultipartFile) Read(buffer []byte) (int, error) {
	readCount, err := file.part.Read(buffer)
	if !errors.Is(err, io.EOF) || file.validated {
		return readCount, err
	}

	file.validated = true
	additionalPart, nextErr := file.reader.NextPart()
	if additionalPart != nil {
		_ = additionalPart.Close()
	}
	if !errors.Is(nextErr, io.EOF) || additionalPart != nil {
		return readCount, errors.New("unexpected multipart data after file")
	}

	return readCount, io.EOF
}

func (handler *mediaUploadHandler) upload(
	response http.ResponseWriter,
	request *http.Request,
) {
	actor, exists := mediaActor(request)
	if !exists {
		writeMediaContextError(response)

		return
	}

	item, ok := handler.processUpload(response, request, actor)
	if !ok {
		return
	}

	response.Header().Set("Location", "/api/v1/media/"+item.ID)
	writeMediaMetadataResponse(response, item, http.StatusCreated)
}

func (handler *mediaUploadHandler) processUpload(
	response http.ResponseWriter,
	request *http.Request,
	actor identity.User,
) (domainmedia.Item, bool) {
	mediaType, parameters, err := mime.ParseMediaType(
		request.Header.Get("Content-Type"),
	)
	if err != nil || mediaType != "multipart/form-data" || parameters["boundary"] == "" {
		writeInvalidRequest(response)

		return domainmedia.Item{}, false
	}

	request.Body = http.MaxBytesReader(
		response,
		request.Body,
		handler.maximumSize+maximumMultipartOverhead,
	)
	reader := multipart.NewReader(request.Body, parameters["boundary"])
	metadataPart, err := reader.NextPart()
	if err != nil || metadataPart.FormName() != "metadata" || metadataPart.FileName() != "" {
		writeInvalidRequest(response)

		return domainmedia.Item{}, false
	}
	metadata, err := decodeUploadMetadata(metadataPart)
	_ = metadataPart.Close()
	if err != nil {
		writeInvalidRequest(response)

		return domainmedia.Item{}, false
	}

	filePart, err := reader.NextPart()
	if err != nil || filePart.FormName() != "file" {
		writeInvalidRequest(response)

		return domainmedia.Item{}, false
	}
	filename, mimeType, err := uploadFileMetadata(filePart)
	if err != nil {
		_ = filePart.Close()
		writeInvalidRequest(response)

		return domainmedia.Item{}, false
	}

	input := appmedia.UploadInput{
		Title:            metadata.Title,
		Authors:          metadata.Authors,
		OriginalFilename: filename,
		Type:             metadata.Type,
		MIMEType:         mimeType,
		Source: &finalMultipartFile{
			part:   filePart,
			reader: reader,
		},
	}

	// The service consumes the file part synchronously while the multipart
	// reader and request body remain open.
	item, err := handler.uploads.UploadItem(request.Context(), actor, input)
	_ = filePart.Close()
	if err != nil {
		writeMediaUploadError(response, err)

		return domainmedia.Item{}, false
	}

	return item, true
}

func decodeUploadMetadata(part *multipart.Part) (mediaUploadMetadata, error) {
	mediaType, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return mediaUploadMetadata{}, errors.New("expected JSON metadata part")
	}
	document, err := io.ReadAll(io.LimitReader(part, maximumJSONBodySize+1))
	if err != nil {
		return mediaUploadMetadata{}, fmt.Errorf("read upload metadata: %w", err)
	}
	if len(document) > maximumJSONBodySize {
		return mediaUploadMetadata{}, errors.New("upload metadata is too large")
	}
	var metadata mediaUploadMetadata
	if err := decodeSingleJSONDocument(document, &metadata); err != nil {
		return mediaUploadMetadata{}, fmt.Errorf("decode upload metadata: %w", err)
	}

	return metadata, nil
}

func decodeSingleJSONDocument(document []byte, destination any) error {
	decoder := json.NewDecoder(strings.NewReader(string(document)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}

	return ensureJSONEnd(decoder)
}

func uploadFileMetadata(part *multipart.Part) (string, string, error) {
	_, parameters, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
	if err != nil || parameters["filename"] == "" {
		return "", "", errors.New("missing upload filename")
	}
	filename := parameters["filename"]
	if strings.ContainsAny(filename, `/\:`) {
		return "", "", errors.New("upload filename contains path syntax")
	}
	mediaType, mediaParameters, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
	if err != nil || len(mediaParameters) != 0 {
		return "", "", errors.New("invalid upload media type")
	}

	return filename, mediaType, nil
}

func writeMediaUploadError(response http.ResponseWriter, inputError error) {
	if errors.Is(inputError, appmedia.ErrUploadCompensationFailed) {
		writeMediaContextError(response)

		return
	}
	if errors.Is(inputError, content.ErrConflict) ||
		errors.Is(inputError, content.ErrLocationConflict) {
		writeJSONError(response, http.StatusConflict, "conflict", "Resource conflict.")

		return
	}
	var maximumBodyError *http.MaxBytesError
	if errors.Is(inputError, content.ErrTooLarge) ||
		errors.As(inputError, &maximumBodyError) {
		writeJSONError(
			response,
			http.StatusRequestEntityTooLarge,
			"content_too_large",
			"Uploaded content is too large.",
		)

		return
	}
	if errors.Is(inputError, content.ErrEmpty) ||
		errors.Is(inputError, content.ErrInvalidSource) {
		writeInvalidRequest(response)

		return
	}
	writeMediaApplicationError(response, inputError)
}
