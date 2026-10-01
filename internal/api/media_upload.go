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
	argResolver SessionResolver,
	argService MediaUploadService,
	argMaximumSize int64,
) Option {
	return func(argConfiguration *handlerConfiguration) {
		argConfiguration.mediaUploadResolver = argResolver
		argConfiguration.mediaUploads = argService
		argConfiguration.maximumUploadSize = argMaximumSize
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

func (file *finalMultipartFile) Read(argBuffer []byte) (int, error) {
	readCount, err := file.part.Read(argBuffer)
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
	argResponse http.ResponseWriter,
	argRequest *http.Request,
) {
	actor, exists := mediaActor(argRequest)
	if !exists {
		writeMediaContextError(argResponse)

		return
	}

	item, ok := handler.processUpload(argResponse, argRequest, actor)
	if !ok {
		return
	}

	argResponse.Header().Set("Location", "/api/v1/media/"+item.ID)
	writeMediaMetadataResponse(argResponse, item, http.StatusCreated)
}

func (handler *mediaUploadHandler) processUpload(
	argResponse http.ResponseWriter,
	argRequest *http.Request,
	argActor identity.User,
) (domainmedia.Item, bool) {
	mediaType, parameters, err := mime.ParseMediaType(
		argRequest.Header.Get("Content-Type"),
	)
	if err != nil || mediaType != "multipart/form-data" || parameters["boundary"] == "" {
		writeInvalidRequest(argResponse)

		return domainmedia.Item{}, false
	}

	argRequest.Body = http.MaxBytesReader(
		argResponse,
		argRequest.Body,
		handler.maximumSize+maximumMultipartOverhead,
	)
	reader := multipart.NewReader(argRequest.Body, parameters["boundary"])
	metadataPart, err := reader.NextPart()
	if err != nil || metadataPart.FormName() != "metadata" || metadataPart.FileName() != "" {
		writeInvalidRequest(argResponse)

		return domainmedia.Item{}, false
	}
	metadata, err := decodeUploadMetadata(metadataPart)
	_ = metadataPart.Close()
	if err != nil {
		writeInvalidRequest(argResponse)

		return domainmedia.Item{}, false
	}

	filePart, err := reader.NextPart()
	if err != nil || filePart.FormName() != "file" {
		writeInvalidRequest(argResponse)

		return domainmedia.Item{}, false
	}
	filename, mimeType, err := uploadFileMetadata(filePart)
	if err != nil {
		_ = filePart.Close()
		writeInvalidRequest(argResponse)

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
	item, err := handler.uploads.UploadItem(argRequest.Context(), argActor, input)
	_ = filePart.Close()
	if err != nil {
		writeMediaUploadError(argResponse, err)

		return domainmedia.Item{}, false
	}

	return item, true
}

func decodeUploadMetadata(argPart *multipart.Part) (mediaUploadMetadata, error) {
	mediaType, _, err := mime.ParseMediaType(argPart.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return mediaUploadMetadata{}, errors.New("expected JSON metadata part")
	}
	document, err := io.ReadAll(io.LimitReader(argPart, maximumJSONBodySize+1))
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

func decodeSingleJSONDocument(argDocument []byte, argDestination any) error {
	decoder := json.NewDecoder(strings.NewReader(string(argDocument)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(argDestination); err != nil {
		return err
	}

	return ensureJSONEnd(decoder)
}

func uploadFileMetadata(argPart *multipart.Part) (string, string, error) {
	_, parameters, err := mime.ParseMediaType(argPart.Header.Get("Content-Disposition"))
	if err != nil || parameters["filename"] == "" {
		return "", "", errors.New("missing upload filename")
	}
	filename := parameters["filename"]
	if strings.ContainsAny(filename, `/\:`) {
		return "", "", errors.New("upload filename contains path syntax")
	}
	mediaType, mediaParameters, err := mime.ParseMediaType(argPart.Header.Get("Content-Type"))
	if err != nil || len(mediaParameters) != 0 {
		return "", "", errors.New("invalid upload media type")
	}

	return filename, mediaType, nil
}

func writeMediaUploadError(argResponse http.ResponseWriter, argError error) {
	if errors.Is(argError, appmedia.ErrUploadCompensationFailed) {
		writeMediaContextError(argResponse)

		return
	}
	if errors.Is(argError, content.ErrConflict) ||
		errors.Is(argError, content.ErrLocationConflict) {
		writeJSONError(argResponse, http.StatusConflict, "conflict", "Resource conflict.")

		return
	}
	var maximumBodyError *http.MaxBytesError
	if errors.Is(argError, content.ErrTooLarge) ||
		errors.As(argError, &maximumBodyError) {
		writeJSONError(
			argResponse,
			http.StatusRequestEntityTooLarge,
			"content_too_large",
			"Uploaded content is too large.",
		)

		return
	}
	if errors.Is(argError, content.ErrEmpty) ||
		errors.Is(argError, content.ErrInvalidSource) {
		writeInvalidRequest(argResponse)

		return
	}
	writeMediaApplicationError(argResponse, argError)
}
