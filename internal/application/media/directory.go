package media

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ebe542/go-mediaarchive/internal/identity"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

const (
	// DefaultDirectoryLimit is used when no page size is selected.
	DefaultDirectoryLimit = 50
	// MaximumDirectoryLimit bounds database and response work for one page.
	MaximumDirectoryLimit       = 100
	maximumDirectoryQueryLength = 200
)

var (
	// ErrDirectoryForbidden indicates that an inactive actor cannot browse media.
	ErrDirectoryForbidden = errors.New("media directory forbidden")
	// ErrInvalidDirectoryFilter indicates unsupported or unsafe search criteria.
	ErrInvalidDirectoryFilter = errors.New("invalid media directory filter")
	// ErrInvalidDirectoryLimit indicates an unsupported page size.
	ErrInvalidDirectoryLimit = errors.New("invalid media directory limit")
	// ErrInvalidDirectoryCursor indicates an unusable pagination position.
	ErrInvalidDirectoryCursor = errors.New("invalid media directory cursor")
)

// DirectoryFilter selects discoverable media by minimal metadata.
type DirectoryFilter struct {
	Query string
	Type  domainmedia.Type
}

// DirectoryCursor identifies the immutable key after the last returned item.
type DirectoryCursor struct {
	CreatedAt  time.Time
	ID         string
	FilterHash [sha256.Size]byte
	Limit      int
}

// NewDirectoryCursor validates a media-directory pagination position.
func NewDirectoryCursor(
	createdAt time.Time,
	id string,
	filterHash [sha256.Size]byte,
	limit int,
) (DirectoryCursor, error) {
	if createdAt.IsZero() {
		return DirectoryCursor{}, fmt.Errorf(
			"%w: creation time must not be zero",
			ErrInvalidDirectoryCursor,
		)
	}
	if err := domainmedia.ValidateMediaID(id); err != nil {
		return DirectoryCursor{}, fmt.Errorf("%w: %v", ErrInvalidDirectoryCursor, err)
	}
	if filterHash == ([sha256.Size]byte{}) ||
		limit < 1 || limit > MaximumDirectoryLimit {
		return DirectoryCursor{}, ErrInvalidDirectoryCursor
	}

	return DirectoryCursor{
		CreatedAt:  createdAt.UTC(),
		ID:         id,
		FilterHash: filterHash,
		Limit:      limit,
	}, nil
}

// ListItemsInput selects one bounded media-directory page.
type ListItemsInput struct {
	Filter DirectoryFilter
	Limit  int
	Cursor *DirectoryCursor
}

// ItemPage contains discoverable media and an optional continuation position.
type ItemPage struct {
	Items      []domainmedia.Item
	NextCursor *DirectoryCursor
}

// DirectoryRepository loads only media discoverable by one actor.
type DirectoryRepository interface {
	ListDiscoverableMedia(
		context.Context,
		string,
		DirectoryFilter,
		*DirectoryCursor,
		int,
	) ([]domainmedia.Item, error)
}

// Directory provides filtered, authorization-aware media discovery.
type Directory struct {
	repository DirectoryRepository
}

// NewDirectory creates a media-directory service.
func NewDirectory(repository DirectoryRepository) *Directory {
	return &Directory{repository: repository}
}

// ListItems retrieves one validated page without exposing hidden media.
func (directory *Directory) ListItems(
	ctx context.Context,
	actor identity.User,
	input ListItemsInput,
) (ItemPage, error) {
	if !actor.Active {
		return ItemPage{}, ErrDirectoryForbidden
	}
	if err := identity.ValidateUserID(actor.ID); err != nil {
		return ItemPage{}, fmt.Errorf("%w: actor: %v", ErrInvalidDirectoryFilter, err)
	}

	filter, err := normalizeDirectoryFilter(input.Filter)
	if err != nil {
		return ItemPage{}, err
	}
	limit := input.Limit
	if limit == 0 {
		limit = DefaultDirectoryLimit
	}
	if limit < 1 || limit > MaximumDirectoryLimit {
		return ItemPage{}, fmt.Errorf(
			"%w: expected a value from 1 through %d",
			ErrInvalidDirectoryLimit,
			MaximumDirectoryLimit,
		)
	}
	filterHash, err := directoryFilterFingerprint(filter, limit)
	if err != nil {
		return ItemPage{}, err
	}
	if input.Cursor != nil {
		cursor, err := NewDirectoryCursor(
			input.Cursor.CreatedAt,
			input.Cursor.ID,
			input.Cursor.FilterHash,
			input.Cursor.Limit,
		)
		if err != nil || cursor.FilterHash != filterHash || cursor.Limit != limit {
			return ItemPage{}, ErrInvalidDirectoryCursor
		}
		input.Cursor = &cursor
	}

	items, err := directory.repository.ListDiscoverableMedia(
		ctx,
		actor.ID,
		filter,
		input.Cursor,
		limit+1,
	)
	if err != nil {
		return ItemPage{}, fmt.Errorf("retrieve media directory: %w", err)
	}

	page := ItemPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = directoryCursorForItem(last, filterHash, limit)
	if err != nil {
		return ItemPage{}, err
	}

	return page, nil
}

func normalizeDirectoryFilter(filter DirectoryFilter) (DirectoryFilter, error) {
	filter.Query = strings.TrimSpace(filter.Query)
	if utf8.RuneCountInString(filter.Query) > maximumDirectoryQueryLength {
		return DirectoryFilter{}, fmt.Errorf(
			"%w: query exceeds %d characters",
			ErrInvalidDirectoryFilter,
			maximumDirectoryQueryLength,
		)
	}
	for _, character := range filter.Query {
		if unicode.IsControl(character) {
			return DirectoryFilter{}, fmt.Errorf(
				"%w: query contains a control character",
				ErrInvalidDirectoryFilter,
			)
		}
	}
	if filter.Type != "" && !filter.Type.Valid() {
		return DirectoryFilter{}, fmt.Errorf(
			"%w: unsupported media type %q",
			ErrInvalidDirectoryFilter,
			filter.Type,
		)
	}

	return filter, nil
}

func directoryCursorForItem(
	item domainmedia.Item,
	filterHash [sha256.Size]byte,
	limit int,
) (*DirectoryCursor, error) {
	cursor, err := NewDirectoryCursor(item.CreatedAt, item.ID, filterHash, limit)
	if err != nil {
		return nil, fmt.Errorf("create media directory cursor: %w", err)
	}

	return &cursor, nil
}

func directoryFilterFingerprint(
	filter DirectoryFilter,
	limit int,
) ([sha256.Size]byte, error) {
	document := struct {
		Query string           `json:"query"`
		Type  domainmedia.Type `json:"type"`
		Limit int              `json:"limit"`
	}{Query: filter.Query, Type: filter.Type, Limit: limit}
	encoded, err := json.Marshal(document)
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode media directory filter: %w", err)
	}

	return sha256.Sum256(encoded), nil
}
