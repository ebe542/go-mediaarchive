package media_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	appmedia "github.com/ebe542/go-mediaarchive/internal/application/media"
	"github.com/ebe542/go-mediaarchive/internal/content"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

type recordingContentLocationFinder struct {
	location content.Location
	err      error
	calls    int
	mediaID  string
}

func (finder *recordingContentLocationFinder) FindByMediaID(
	_ context.Context,
	mediaID string,
) (content.Location, error) {
	finder.calls++
	finder.mediaID = mediaID

	return finder.location, finder.err
}

type recordingContentReadStore struct {
	opened     content.Opened
	err        error
	calls      int
	storageKey string
}

func (store *recordingContentReadStore) Open(
	_ context.Context,
	storageKey string,
) (content.Opened, error) {
	store.calls++
	store.storageKey = storageKey

	return store.opened, store.err
}

type trackedReadSeekCloser struct {
	*bytes.Reader
	closed bool
	err    error
}

func (reader *trackedReadSeekCloser) Close() error {
	reader.closed = true

	return reader.err
}

func TestContentReadServiceOpensOwnedContent(t *testing.T) {
	item := serviceItem(t)
	location := contentLocation(t, item.ID)
	reader := &trackedReadSeekCloser{Reader: bytes.NewReader([]byte("content"))}
	locations := &recordingContentLocationFinder{location: location}
	store := &recordingContentReadStore{
		opened: content.Opened{
			Reader:       reader,
			Size:         item.Size,
			LastModified: serviceTime(),
		},
	}
	grants := &recordingGrantFinder{err: domainmedia.ErrGrantNotFound}
	service := appmedia.NewContentReadService(
		&recordingMediaRepository{item: item},
		grants,
		locations,
		store,
	)

	opened, err := service.Open(
		context.Background(),
		serviceUser(item.OwnerID, identity.RoleViewer, true),
		item.ID,
	)
	if err != nil {
		t.Fatalf("open owned content: %v", err)
	}
	if opened.Item.ID != item.ID || opened.Item.OwnerID != item.OwnerID ||
		opened.Size != item.Size ||
		!opened.LastModified.Equal(serviceTime()) {
		t.Fatalf("unexpected opened content: %+v", opened)
	}
	if grants.calls != 0 {
		t.Fatalf("expected no owner grant lookup, got %d", grants.calls)
	}
	if locations.calls != 1 || locations.mediaID != item.ID {
		t.Fatalf("unexpected location lookup: %+v", locations)
	}
	if store.calls != 1 || store.storageKey != location.StorageKey {
		t.Fatalf("unexpected content open: %+v", store)
	}
	if err := opened.Reader.Close(); err != nil {
		t.Fatalf("close opened content: %v", err)
	}
	if !reader.closed {
		t.Fatal("expected caller to own the returned reader")
	}
}

func TestContentReadServiceAcceptsExplicitReadGrant(t *testing.T) {
	item := serviceItem(t)
	grant, err := domainmedia.NewGrant(
		item.ID,
		serviceActorID,
		servicePermissionSet(t, domainmedia.PermissionRead),
	)
	if err != nil {
		t.Fatalf("create read grant: %v", err)
	}
	reader := &trackedReadSeekCloser{Reader: bytes.NewReader(nil)}
	service := appmedia.NewContentReadService(
		&recordingMediaRepository{item: item},
		&recordingGrantFinder{grant: grant},
		&recordingContentLocationFinder{location: contentLocation(t, item.ID)},
		&recordingContentReadStore{
			opened: content.Opened{Reader: reader, Size: item.Size},
		},
	)

	opened, err := service.Open(
		context.Background(),
		serviceUser(serviceActorID, identity.RoleViewer, true),
		item.ID,
	)
	if err != nil {
		t.Fatalf("open granted content: %v", err)
	}
	if err := opened.Reader.Close(); err != nil {
		t.Fatalf("close granted content: %v", err)
	}
}

func TestContentReadServiceAuthorizesBeforeContentLookup(t *testing.T) {
	item := serviceItem(t)
	downloadGrant, err := domainmedia.NewGrant(
		item.ID,
		serviceActorID,
		servicePermissionSet(t, domainmedia.PermissionDownload),
	)
	if err != nil {
		t.Fatalf("create download grant: %v", err)
	}

	testCases := []struct {
		name       string
		repository *recordingMediaRepository
		grants     *recordingGrantFinder
	}{
		{
			name:       "unknown medium",
			repository: &recordingMediaRepository{findError: domainmedia.ErrItemNotFound},
			grants:     &recordingGrantFinder{},
		},
		{
			name:       "download without read",
			repository: &recordingMediaRepository{item: item},
			grants:     &recordingGrantFinder{grant: downloadGrant},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			locations := &recordingContentLocationFinder{}
			store := &recordingContentReadStore{}
			service := appmedia.NewContentReadService(
				testCase.repository,
				testCase.grants,
				locations,
				store,
			)

			_, err := service.Open(
				context.Background(),
				serviceUser(serviceActorID, identity.RoleAdmin, true),
				item.ID,
			)
			if !errors.Is(err, appmedia.ErrMediaNotFound) {
				t.Fatalf("expected ErrMediaNotFound, got %v", err)
			}
			if locations.calls != 0 || store.calls != 0 {
				t.Fatalf(
					"expected no content lookup, got locations=%d store=%d",
					locations.calls,
					store.calls,
				)
			}
		})
	}
}

func TestContentReadServiceMasksUnavailableContent(t *testing.T) {
	item := serviceItem(t)
	location := contentLocation(t, item.ID)
	testCases := []struct {
		name          string
		locations     *recordingContentLocationFinder
		store         *recordingContentReadStore
		expectedCalls int
	}{
		{
			name:      "missing location",
			locations: &recordingContentLocationFinder{err: content.ErrLocationNotFound},
			store:     &recordingContentReadStore{},
		},
		{
			name:          "missing file",
			locations:     &recordingContentLocationFinder{location: location},
			store:         &recordingContentReadStore{err: content.ErrNotFound},
			expectedCalls: 1,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			service := appmedia.NewContentReadService(
				&recordingMediaRepository{item: item},
				&recordingGrantFinder{},
				testCase.locations,
				testCase.store,
			)

			_, err := service.Open(
				context.Background(),
				serviceUser(item.OwnerID, identity.RoleViewer, true),
				item.ID,
			)
			if !errors.Is(err, appmedia.ErrMediaNotFound) {
				t.Fatalf("expected ErrMediaNotFound, got %v", err)
			}
			if testCase.store.calls != testCase.expectedCalls {
				t.Fatalf(
					"expected %d store calls, got %d",
					testCase.expectedCalls,
					testCase.store.calls,
				)
			}
		})
	}
}

func TestContentReadServicePreservesOperationalErrors(t *testing.T) {
	item := serviceItem(t)
	locationErr := errors.New("location unavailable")
	storeErr := errors.New("storage unavailable")
	testCases := []struct {
		name      string
		locations *recordingContentLocationFinder
		store     *recordingContentReadStore
		expected  error
	}{
		{
			name:      "location failure",
			locations: &recordingContentLocationFinder{err: locationErr},
			store:     &recordingContentReadStore{},
			expected:  locationErr,
		},
		{
			name:      "storage failure",
			locations: &recordingContentLocationFinder{location: contentLocation(t, item.ID)},
			store:     &recordingContentReadStore{err: storeErr},
			expected:  storeErr,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			service := appmedia.NewContentReadService(
				&recordingMediaRepository{item: item},
				&recordingGrantFinder{},
				testCase.locations,
				testCase.store,
			)

			_, err := service.Open(
				context.Background(),
				serviceUser(item.OwnerID, identity.RoleViewer, true),
				item.ID,
			)
			if !errors.Is(err, testCase.expected) {
				t.Fatalf("expected wrapped %v, got %v", testCase.expected, err)
			}
		})
	}
}

func TestContentReadServiceRejectsInconsistentContent(t *testing.T) {
	item := serviceItem(t)
	location := contentLocation(t, item.ID)

	t.Run("location belongs to another medium", func(t *testing.T) {
		otherLocation := contentLocation(t, serviceActorID)
		store := &recordingContentReadStore{}
		service := appmedia.NewContentReadService(
			&recordingMediaRepository{item: item},
			&recordingGrantFinder{},
			&recordingContentLocationFinder{location: otherLocation},
			store,
		)

		_, err := service.Open(
			context.Background(),
			serviceUser(item.OwnerID, identity.RoleViewer, true),
			item.ID,
		)
		if !errors.Is(err, appmedia.ErrContentIntegrity) {
			t.Fatalf("expected ErrContentIntegrity, got %v", err)
		}
		if store.calls != 0 {
			t.Fatalf("expected inconsistent location not to reach storage, got %d calls", store.calls)
		}
	})

	t.Run("stored size differs", func(t *testing.T) {
		reader := &trackedReadSeekCloser{Reader: bytes.NewReader(nil)}
		service := appmedia.NewContentReadService(
			&recordingMediaRepository{item: item},
			&recordingGrantFinder{},
			&recordingContentLocationFinder{location: location},
			&recordingContentReadStore{
				opened: content.Opened{Reader: reader, Size: item.Size + 1},
			},
		)

		_, err := service.Open(
			context.Background(),
			serviceUser(item.OwnerID, identity.RoleViewer, true),
			item.ID,
		)
		if !errors.Is(err, appmedia.ErrContentIntegrity) {
			t.Fatalf("expected ErrContentIntegrity, got %v", err)
		}
		if !reader.closed {
			t.Fatal("expected inconsistent opened content to be closed")
		}
	})
}

func contentLocation(test *testing.T, mediaID string) content.Location {
	test.Helper()

	location, err := content.NewLocation(
		mediaID,
		mediaID[:2]+"/"+mediaID,
		serviceTime(),
	)
	if err != nil {
		test.Fatalf("create content location fixture: %v", err)
	}

	return location
}

var _ io.ReadSeekCloser = (*trackedReadSeekCloser)(nil)
