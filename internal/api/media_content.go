package api

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"

	appmedia "github.com/ebe542/go-mediaarchive/internal/application/media"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

// MediaContentService opens authorized managed content for streaming.
type MediaContentService interface {
	Open(
		ctx context.Context,
		actor identity.User,
		mediaID string,
	) (appmedia.ReadableContent, error)
}

// WithMediaContentAPI enables authenticated inline content streaming.
func WithMediaContentAPI(
	resolver SessionResolver,
	service MediaContentService,
) Option {
	return func(configuration *handlerConfiguration) {
		configuration.mediaContentResolver = resolver
		configuration.mediaContent = service
	}
}

type mediaContentHandler struct {
	content MediaContentService
}

func (handler *mediaContentHandler) serve(
	response http.ResponseWriter,
	request *http.Request,
) {
	actor, exists := mediaActor(request)
	if !exists {
		writeMediaContextError(response)

		return
	}

	opened, err := handler.content.Open(
		request.Context(),
		actor,
		request.PathValue("id"),
	)
	if err != nil {
		writeMediaApplicationError(response, err)

		return
	}
	defer opened.Reader.Close()

	selected, partial, err := parseContentRange(
		request.Header.Values("Range"),
		opened.Size,
	)
	if err != nil {
		response.Header().Set(
			"Content-Range",
			fmt.Sprintf("bytes */%d", opened.Size),
		)
		writeJSONError(
			response,
			http.StatusRequestedRangeNotSatisfiable,
			"invalid_range",
			"Requested range is not satisfiable.",
		)

		return
	}

	if request.Method == http.MethodGet && selected.start > 0 {
		offset, seekErr := opened.Reader.Seek(selected.start, io.SeekStart)
		if seekErr != nil || offset != selected.start {
			writeMediaContextError(response)

			return
		}
	}

	writeContentHeaders(response, opened, selected, partial)
	status := http.StatusOK
	if partial {
		status = http.StatusPartialContent
	}
	response.WriteHeader(status)
	if request.Method == http.MethodHead {
		return
	}

	_, _ = io.CopyN(response, opened.Reader, selected.length)
}

func writeContentHeaders(
	response http.ResponseWriter,
	opened appmedia.ReadableContent,
	selected contentRange,
	partial bool,
) {
	response.Header().Set("Content-Type", opened.Item.MIMEType)
	response.Header().Set("Content-Length", strconv.FormatInt(selected.length, 10))
	response.Header().Set(
		"Content-Disposition",
		mime.FormatMediaType(
			"inline",
			map[string]string{"filename": opened.Item.OriginalFilename},
		),
	)
	response.Header().Set("Cache-Control", "private, no-store")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set(
		"ETag",
		`"`+hex.EncodeToString(opened.Item.Checksum[:])+`"`,
	)
	response.Header().Set("Last-Modified", opened.LastModified.UTC().Format(http.TimeFormat))
	response.Header().Set("Accept-Ranges", "bytes")
	if partial {
		response.Header().Set(
			"Content-Range",
			fmt.Sprintf(
				"bytes %d-%d/%d",
				selected.start,
				selected.start+selected.length-1,
				opened.Size,
			),
		)
	}
}
