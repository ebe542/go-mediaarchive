package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
)

const maximumJSONBodySize = 64 * 1024

func decodeJSONRequest(
	response http.ResponseWriter,
	request *http.Request,
	destination any,
) error {
	mediaType, _, err := mime.ParseMediaType(
		request.Header.Get("Content-Type"),
	)
	if err != nil || mediaType != "application/json" {
		return errors.New("expected application/json content type")
	}

	request.Body = http.MaxBytesReader(
		response,
		request.Body,
		maximumJSONBodySize,
	)

	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode JSON request: %w", err)
	}

	if err := ensureJSONEnd(decoder); err != nil {
		return err
	}

	return nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var additionalValue any

	if err := decoder.Decode(&additionalValue); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("additional JSON value")
		}

		return fmt.Errorf("decode trailing JSON: %w", err)
	}

	return nil
}
