package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	sharedcli "github.com/ebe542/go-mediaarchive/internal/cli"
	apiclient "github.com/ebe542/go-mediaarchive/internal/client"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

const (
	defaultMediaLimit       = 50
	maximumMediaLimit       = 100
	maximumMediaQueryLength = 200
)

func (console *userConsole) media(
	ctx context.Context,
	arguments []string,
) error {
	if !console.session.Authenticated() {
		return errors.New("not logged in")
	}
	if len(arguments) == 0 {
		return commandUsage("media list [limit]|search [limit]|next|first|get <id>")
	}

	switch arguments[0] {
	case "list":
		return console.startMediaList(ctx, arguments[1:])
	case "search":
		return console.startMediaSearch(ctx, arguments[1:])
	case "next":
		if len(arguments) != 1 {
			return commandUsage("media next")
		}

		return console.nextMediaPage(ctx)
	case "first":
		if len(arguments) != 1 {
			return commandUsage("media first")
		}

		return console.firstMediaPage(ctx)
	case "get":
		if len(arguments) != 2 {
			return commandUsage("media get <id>")
		}

		return console.getMedia(ctx, arguments[1])
	default:
		return fmt.Errorf("unknown media command %q", arguments[0])
	}
}

func (console *userConsole) startMediaList(
	ctx context.Context,
	arguments []string,
) error {
	limit, err := parseMediaLimit(arguments, "media list")
	if err != nil {
		return err
	}

	return console.startMediaPage(ctx, apiclient.MediaDirectoryFilter{Limit: limit})
}

func (console *userConsole) startMediaSearch(
	ctx context.Context,
	arguments []string,
) error {
	limit, err := parseMediaLimit(arguments, "media search")
	if err != nil {
		return err
	}
	query, err := console.readMediaQuery(ctx)
	if err != nil {
		return err
	}
	mediaType, err := console.readMediaType(ctx)
	if err != nil {
		return err
	}

	return console.startMediaPage(ctx, apiclient.MediaDirectoryFilter{
		Query: query,
		Type:  mediaType,
		Limit: limit,
	})
}

func parseMediaLimit(arguments []string, command string) (int, error) {
	if len(arguments) > 1 {
		return 0, commandUsage(command + " [limit]")
	}
	if len(arguments) == 0 {
		return defaultMediaLimit, nil
	}
	limit, err := strconv.Atoi(arguments[0])
	if err != nil || limit < 1 || limit > maximumMediaLimit {
		return 0, fmt.Errorf("media page limit must be between 1 and %d", maximumMediaLimit)
	}

	return limit, nil
}

func (console *userConsole) readMediaQuery(ctx context.Context) (string, error) {
	for {
		value, available, err := console.readCommand(ctx, "Search title or author: ")
		if err != nil {
			return "", fmt.Errorf("read media query: %w", err)
		}
		if !available {
			return "", io.EOF
		}
		value = strings.TrimSpace(value)
		if value == "" {
			console.printError(errors.New("search text is required; try again"))

			continue
		}
		if utf8.RuneCountInString(value) > maximumMediaQueryLength {
			console.printError(fmt.Errorf(
				"search text must not exceed %d characters; try again",
				maximumMediaQueryLength,
			))

			continue
		}
		valid := true
		for _, character := range value {
			if unicode.IsControl(character) {
				valid = false

				break
			}
		}
		if !valid {
			console.printError(errors.New(
				"search text must not contain control characters; try again",
			))

			continue
		}

		return value, nil
	}
}

func (console *userConsole) readMediaType(ctx context.Context) (domainmedia.Type, error) {
	for {
		value, available, err := console.readCommand(
			ctx,
			"Media type (book|document|video, blank for any): ",
		)
		if err != nil {
			return "", fmt.Errorf("read media type: %w", err)
		}
		if !available {
			return "", io.EOF
		}
		mediaType := domainmedia.Type(strings.TrimSpace(value))
		if mediaType == "" || mediaType.Valid() {
			return mediaType, nil
		}

		console.printError(errors.New(
			"media type must be book, document, video, or blank; try again",
		))
	}
}

func (console *userConsole) startMediaPage(
	ctx context.Context,
	filter apiclient.MediaDirectoryFilter,
) error {
	if err := console.loadMediaPage(ctx, filter); err != nil {
		return err
	}
	console.mediaFilter = filter
	console.mediaStarted = true

	return nil
}

func (console *userConsole) nextMediaPage(ctx context.Context) error {
	if !console.mediaStarted {
		return errors.New("start pagination with 'media list [limit]' or 'media search [limit]'")
	}
	if console.mediaCursor == "" {
		return errors.New("there is no next media page")
	}

	filter := console.mediaFilter
	filter.Cursor = console.mediaCursor

	return console.loadMediaPage(ctx, filter)
}

func (console *userConsole) firstMediaPage(ctx context.Context) error {
	if !console.mediaStarted {
		return errors.New("start pagination with 'media list [limit]' or 'media search [limit]'")
	}

	return console.loadMediaPage(ctx, console.mediaFilter)
}

func (console *userConsole) loadMediaPage(
	ctx context.Context,
	filter apiclient.MediaDirectoryFilter,
) error {
	page, err := console.api.ListMedia(
		ctx,
		console.session.AccessToken(),
		filter,
	)
	if err != nil {
		return fmt.Errorf("list media: %w", err)
	}
	console.mediaCursor = page.NextCursor
	if len(page.Items) == 0 {
		fmt.Fprintln(console.output, "No media found.")

		return nil
	}
	for _, item := range page.Items {
		console.printMediaSummary(item)
	}
	if page.NextCursor != "" {
		fmt.Fprintln(console.output, "More media are available; use 'media next'.")
	}

	return nil
}

func (console *userConsole) getMedia(ctx context.Context, id string) error {
	if err := domainmedia.ValidateMediaID(id); err != nil {
		return errors.New("media ID must be a canonical UUID")
	}
	item, err := console.api.MediaByID(
		ctx,
		console.session.AccessToken(),
		id,
	)
	if err != nil {
		return fmt.Errorf("get media: %w", err)
	}
	console.printMedia(item)

	return nil
}

func (console *userConsole) printMediaSummary(item apiclient.Media) {
	authors := strings.Join(item.Authors, ", ")
	if authors == "" {
		authors = "-"
	}
	fmt.Fprintf(
		console.output,
		"%s  %-8s  %q  authors=%s  created=%s\n",
		item.ID,
		item.Type,
		item.Title,
		authors,
		sharedcli.FormatLocalTime(item.CreatedAt),
	)
}

func (console *userConsole) printMedia(item apiclient.Media) {
	fmt.Fprintf(console.output, "ID: %s\n", item.ID)
	fmt.Fprintf(console.output, "Title: %s\n", item.Title)
	fmt.Fprintf(console.output, "Authors: %s\n", strings.Join(item.Authors, ", "))
	fmt.Fprintf(console.output, "Type: %s\n", item.Type)
	fmt.Fprintf(console.output, "Original filename: %s\n", item.OriginalFilename)
	fmt.Fprintf(console.output, "MIME type: %s\n", item.MIMEType)
	fmt.Fprintf(console.output, "Size: %d bytes\n", item.Size)
	fmt.Fprintf(console.output, "SHA-256: %s\n", hex.EncodeToString(item.Checksum[:]))
	fmt.Fprintf(console.output, "Owner ID: %s\n", item.OwnerID)
	fmt.Fprintf(console.output, "Created: %s\n", sharedcli.FormatLocalTime(item.CreatedAt))
	fmt.Fprintf(console.output, "Updated: %s\n", sharedcli.FormatLocalTime(item.UpdatedAt))
}

func (console *userConsole) clearMediaPagination() {
	console.mediaFilter = apiclient.MediaDirectoryFilter{}
	console.mediaCursor = ""
	console.mediaStarted = false
}
