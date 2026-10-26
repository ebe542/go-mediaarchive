package media_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	appmedia "github.com/ebe542/go-mediaarchive/internal/application/media"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

type recordingDirectoryRepository struct {
	actorID string
	filter  appmedia.DirectoryFilter
	cursor  *appmedia.DirectoryCursor
	limit   int
	items   []domainmedia.Item
	err     error
}

func (repository *recordingDirectoryRepository) ListDiscoverableMedia(
	_ context.Context,
	actorID string,
	filter appmedia.DirectoryFilter,
	cursor *appmedia.DirectoryCursor,
	limit int,
) ([]domainmedia.Item, error) {
	repository.actorID = actorID
	repository.filter = filter
	repository.cursor = cursor
	repository.limit = limit

	return repository.items, repository.err
}

func TestDirectoryListsNormalizedDiscoverablePage(t *testing.T) {
	first := directoryItem(t, serviceMediaID, serviceTime())
	second := directoryItem(
		t,
		"423e4567-e89b-12d3-a456-426614174000",
		serviceTime().Add(-time.Hour),
	)
	repository := &recordingDirectoryRepository{
		items: []domainmedia.Item{first, second},
	}
	directory := appmedia.NewDirectory(repository)

	page, err := directory.ListItems(
		context.Background(),
		serviceUser(serviceActorID, identity.RoleViewer, true),
		appmedia.ListItemsInput{
			Filter: appmedia.DirectoryFilter{
				Query: "  Security  ",
				Type:  domainmedia.TypeBook,
			},
			Limit: 1,
		},
	)
	if err != nil {
		t.Fatalf("list discoverable media: %v", err)
	}
	if repository.actorID != serviceActorID ||
		repository.filter.Query != "Security" ||
		repository.filter.Type != domainmedia.TypeBook ||
		repository.limit != 2 {
		t.Fatalf("unexpected repository input: %+v", repository)
	}
	if len(page.Items) != 1 || page.Items[0].ID != first.ID {
		t.Fatalf("unexpected media page: %+v", page)
	}
	if page.NextCursor == nil ||
		page.NextCursor.ID != first.ID ||
		!page.NextCursor.CreatedAt.Equal(first.CreatedAt) ||
		page.NextCursor.FilterHash == ([32]byte{}) ||
		page.NextCursor.Limit != 1 {
		t.Fatalf("unexpected next cursor: %+v", page.NextCursor)
	}
	repository.limit = 0
	_, err = directory.ListItems(
		context.Background(),
		serviceUser(serviceActorID, identity.RoleViewer, true),
		appmedia.ListItemsInput{
			Filter: appmedia.DirectoryFilter{Query: "Changed"},
			Limit:  1,
			Cursor: page.NextCursor,
		},
	)
	if !errors.Is(err, appmedia.ErrInvalidDirectoryCursor) {
		t.Fatalf("expected filter-bound cursor rejection, got %v", err)
	}
	if repository.limit != 0 {
		t.Fatal("expected mismatched cursor not to reach repository")
	}
}

func TestDirectoryUsesDefaultLimit(t *testing.T) {
	repository := &recordingDirectoryRepository{items: []domainmedia.Item{}}
	directory := appmedia.NewDirectory(repository)

	page, err := directory.ListItems(
		context.Background(),
		serviceUser(serviceActorID, identity.RoleViewer, true),
		appmedia.ListItemsInput{},
	)
	if err != nil {
		t.Fatalf("list media with default limit: %v", err)
	}
	if page.Items == nil || repository.limit != appmedia.DefaultDirectoryLimit+1 {
		t.Fatalf("unexpected default page: %+v", page)
	}
}

func TestDirectoryValidatesAndNormalizesCursor(t *testing.T) {
	first := directoryItem(t, serviceMediaID, serviceTime())
	second := directoryItem(
		t,
		"423e4567-e89b-12d3-a456-426614174000",
		serviceTime().Add(-time.Hour),
	)
	repository := &recordingDirectoryRepository{items: []domainmedia.Item{first, second}}
	directory := appmedia.NewDirectory(repository)
	firstPage, err := directory.ListItems(
		context.Background(),
		serviceUser(serviceActorID, identity.RoleViewer, true),
		appmedia.ListItemsInput{Limit: 1},
	)
	if err != nil {
		t.Fatalf("create media cursor: %v", err)
	}
	cursor := *firstPage.NextCursor
	cursor.CreatedAt = cursor.CreatedAt.In(time.FixedZone("test", 2*60*60))
	repository.items = []domainmedia.Item{}

	page, err := directory.ListItems(
		context.Background(),
		serviceUser(serviceActorID, identity.RoleViewer, true),
		appmedia.ListItemsInput{Limit: 1, Cursor: &cursor},
	)
	if err != nil {
		t.Fatalf("list media with cursor: %v", err)
	}
	if page.Items == nil || repository.limit != 2 {
		t.Fatalf("unexpected cursor page: %+v", page)
	}
	if repository.cursor == nil || repository.cursor.CreatedAt.Location() != time.UTC {
		t.Fatalf("expected normalized cursor, got %+v", repository.cursor)
	}
}

func TestDirectoryRejectsInvalidInputBeforeRepository(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		actor    identity.User
		input    appmedia.ListItemsInput
		expected error
	}{
		{
			name:     "inactive actor",
			actor:    serviceUser(serviceActorID, identity.RoleViewer, false),
			expected: appmedia.ErrDirectoryForbidden,
		},
		{
			name:  "long query",
			actor: serviceUser(serviceActorID, identity.RoleViewer, true),
			input: appmedia.ListItemsInput{Filter: appmedia.DirectoryFilter{
				Query: strings.Repeat("x", 201),
			}},
			expected: appmedia.ErrInvalidDirectoryFilter,
		},
		{
			name:  "control character",
			actor: serviceUser(serviceActorID, identity.RoleViewer, true),
			input: appmedia.ListItemsInput{Filter: appmedia.DirectoryFilter{
				Query: "unsafe\nquery",
			}},
			expected: appmedia.ErrInvalidDirectoryFilter,
		},
		{
			name:  "media type",
			actor: serviceUser(serviceActorID, identity.RoleViewer, true),
			input: appmedia.ListItemsInput{Filter: appmedia.DirectoryFilter{
				Type: "audio",
			}},
			expected: appmedia.ErrInvalidDirectoryFilter,
		},
		{
			name:     "limit",
			actor:    serviceUser(serviceActorID, identity.RoleViewer, true),
			input:    appmedia.ListItemsInput{Limit: 101},
			expected: appmedia.ErrInvalidDirectoryLimit,
		},
		{
			name:  "cursor filter binding",
			actor: serviceUser(serviceActorID, identity.RoleViewer, true),
			input: appmedia.ListItemsInput{
				Filter: appmedia.DirectoryFilter{Query: "changed"},
				Limit:  1,
				Cursor: &appmedia.DirectoryCursor{
					CreatedAt:  serviceTime(),
					ID:         serviceMediaID,
					FilterHash: [32]byte{1},
					Limit:      1,
				},
			},
			expected: appmedia.ErrInvalidDirectoryCursor,
		},
		{
			name:  "cursor",
			actor: serviceUser(serviceActorID, identity.RoleViewer, true),
			input: appmedia.ListItemsInput{Cursor: &appmedia.DirectoryCursor{
				CreatedAt: serviceTime(), ID: "invalid",
			}},
			expected: appmedia.ErrInvalidDirectoryCursor,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repository := &recordingDirectoryRepository{}
			_, err := appmedia.NewDirectory(repository).ListItems(
				context.Background(), testCase.actor, testCase.input,
			)
			if !errors.Is(err, testCase.expected) {
				t.Fatalf("expected %v, got %v", testCase.expected, err)
			}
			if repository.limit != 0 {
				t.Fatalf("expected no repository call, got %+v", repository)
			}
		})
	}
}

func directoryItem(t *testing.T, id string, createdAt time.Time) domainmedia.Item {
	t.Helper()
	item := serviceItem(t)
	item.ID = id
	item.CreatedAt = createdAt.UTC()
	item.UpdatedAt = createdAt.UTC()

	return item
}
