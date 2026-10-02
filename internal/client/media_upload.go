package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"

	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

// MediaUploadInput contains descriptive metadata and a streaming file source.
type MediaUploadInput struct {
	Title            string
	Authors          []string
	OriginalFilename string
	Type             domainmedia.Type
	MIMEType         string
	Source           io.Reader
}

type mediaUploadRequest struct {
	Title   string           `json:"title"`
	Authors []string         `json:"authors"`
	Type    domainmedia.Type `json:"type"`
}

// UploadMedia streams one file into server-managed content storage.
func (client *Client) UploadMedia(
	argContext context.Context,
	argAccessToken string,
	argInput MediaUploadInput,
) (Media, error) {
	if argInput.Source == nil {
		return Media{}, errors.New("upload media source is required")
	}

	pipeReader, pipeWriter := io.Pipe()
	multipartWriter := multipart.NewWriter(pipeWriter)
	writeResult := make(chan error, 1)
	go func() {
		writeErr := writeMediaUpload(multipartWriter, argInput)
		if closeErr := multipartWriter.Close(); writeErr == nil {
			writeErr = closeErr
		}
		_ = pipeWriter.CloseWithError(writeErr)
		writeResult <- writeErr
	}()

	request, err := http.NewRequestWithContext(
		argContext,
		http.MethodPost,
		client.baseURL+"/api/v1/media/uploads",
		pipeReader,
	)
	if err != nil {
		_ = pipeReader.CloseWithError(err)
		<-writeResult

		return Media{}, fmt.Errorf("create upload media request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	if argAccessToken != "" {
		request.Header.Set("Authorization", "Bearer "+argAccessToken)
	}

	response, requestErr := client.httpClient.Do(request)
	_ = pipeReader.Close()
	writeErr := <-writeResult
	if requestErr != nil {
		return Media{}, fmt.Errorf("request upload media: %w", requestErr)
	}
	defer func() { _ = response.Body.Close() }()

	var responseBody mediaResponse
	responseErr := handleJSONResponse(
		response,
		http.StatusCreated,
		&responseBody,
		"upload media",
	)
	if responseErr != nil {
		return Media{}, responseErr
	}
	if writeErr != nil {
		return Media{}, fmt.Errorf("stream upload media request: %w", writeErr)
	}

	return decodeMediaResponse(responseBody, "upload media")
}

func writeMediaUpload(
	argWriter *multipart.Writer,
	argInput MediaUploadInput,
) error {
	metadataHeader := make(textproto.MIMEHeader)
	metadataHeader.Set("Content-Disposition", `form-data; name="metadata"`)
	metadataHeader.Set("Content-Type", "application/json")
	metadataPart, err := argWriter.CreatePart(metadataHeader)
	if err != nil {
		return fmt.Errorf("create upload metadata part: %w", err)
	}
	if err := json.NewEncoder(metadataPart).Encode(mediaUploadRequest{
		Title:   argInput.Title,
		Authors: append([]string{}, argInput.Authors...),
		Type:    argInput.Type,
	}); err != nil {
		return fmt.Errorf("encode upload metadata: %w", err)
	}

	fileHeader := make(textproto.MIMEHeader)
	fileHeader.Set(
		"Content-Disposition",
		mimeFormatDisposition(argInput.OriginalFilename),
	)
	fileHeader.Set("Content-Type", argInput.MIMEType)
	filePart, err := argWriter.CreatePart(fileHeader)
	if err != nil {
		return fmt.Errorf("create upload file part: %w", err)
	}
	if argInput.Source == nil {
		return errors.New("upload media source is required")
	}
	if _, err := io.Copy(filePart, argInput.Source); err != nil {
		return fmt.Errorf("read upload media source: %w", err)
	}

	return nil
}

func mimeFormatDisposition(argFilename string) string {
	return mime.FormatMediaType(
		"form-data",
		map[string]string{"name": "file", "filename": argFilename},
	)
}
