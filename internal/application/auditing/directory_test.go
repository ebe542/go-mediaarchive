package auditing_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/application/auditing"
	"github.com/ebe542/go-mediaarchive/internal/audit"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

type recordingRepository struct {
	events []audit.Event
	err    error
	limit  int
	filter auditing.Filter
	cursor *auditing.Cursor
}

func (repository *recordingRepository) ListAuditEvents(
	_ context.Context,
	filter auditing.Filter,
	cursor *auditing.Cursor,
	limit int,
) ([]audit.Event, error) {
	repository.filter = filter
	repository.cursor = cursor
	repository.limit = limit
	return repository.events, repository.err
}

type recordingAppender struct {
	event audit.Event
	err   error
	calls int
}

func (appender *recordingAppender) Append(_ context.Context, event audit.Event) error {
	appender.calls++
	appender.event = event
	return appender.err
}

func TestListEventsAuditsAccessAndCreatesBoundCursor(t *testing.T) {
	first := directoryEvent("323e4567-e89b-12d3-a456-426614174000", 12)
	second := directoryEvent("223e4567-e89b-12d3-a456-426614174000", 11)
	repository := &recordingRepository{events: []audit.Event{first, second}}
	appender := &recordingAppender{}
	service := auditing.NewService(
		repository,
		appender,
		func() string { return "423e4567-e89b-12d3-a456-426614174000" },
		func() time.Time { return directoryTime(13) },
	)
	actor := directoryActor()

	page, err := service.ListEvents(
		context.Background(),
		actor,
		auditing.ListInput{
			Filter: auditing.Filter{Outcome: audit.OutcomeDenied},
			Limit:  1,
		},
	)
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	if appender.calls != 1 || appender.event.Type != audit.TypeAuditEventsListed ||
		appender.event.ActorID != actor.ID {
		t.Fatalf("unexpected directory audit event: %+v", appender.event)
	}
	if repository.limit != 2 || repository.filter.Outcome != audit.OutcomeDenied {
		t.Fatalf("unexpected repository request: limit=%d filter=%+v", repository.limit, repository.filter)
	}
	if len(page.Events) != 1 || page.Events[0].ID != first.ID || page.NextCursor == nil {
		t.Fatalf("unexpected audit page: %+v", page)
	}

	nextRepository := &recordingRepository{}
	nextService := auditing.NewService(
		nextRepository,
		&recordingAppender{},
		func() string { return "523e4567-e89b-12d3-a456-426614174000" },
		func() time.Time { return directoryTime(14) },
	)
	_, err = nextService.ListEvents(
		context.Background(),
		actor,
		auditing.ListInput{
			Filter: auditing.Filter{Outcome: audit.OutcomeDenied},
			Limit:  1,
			Cursor: page.NextCursor,
		},
	)
	if err != nil || nextRepository.cursor == nil {
		t.Fatalf("continue audit page: cursor=%+v err=%v", nextRepository.cursor, err)
	}
}

func TestListEventsFailsClosedBeforeQuery(t *testing.T) {
	expectedErr := errors.New("audit unavailable")
	repository := &recordingRepository{}
	service := auditing.NewService(
		repository,
		&recordingAppender{err: expectedErr},
		func() string { return "423e4567-e89b-12d3-a456-426614174000" },
		func() time.Time { return directoryTime(13) },
	)

	if _, err := service.ListEvents(
		context.Background(), directoryActor(), auditing.ListInput{},
	); !errors.Is(err, expectedErr) {
		t.Fatalf("expected audit failure, got %v", err)
	}
	if repository.limit != 0 {
		t.Fatal("expected repository not to be queried after audit failure")
	}
}

func TestListEventsRejectsChangedCursorCriteria(t *testing.T) {
	repository := &recordingRepository{events: []audit.Event{
		directoryEvent("323e4567-e89b-12d3-a456-426614174000", 12),
		directoryEvent("223e4567-e89b-12d3-a456-426614174000", 11),
	}}
	appender := &recordingAppender{}
	service := auditing.NewService(
		repository, appender,
		func() string { return "423e4567-e89b-12d3-a456-426614174000" },
		func() time.Time { return directoryTime(13) },
	)
	page, err := service.ListEvents(
		context.Background(), directoryActor(),
		auditing.ListInput{Filter: auditing.Filter{Outcome: audit.OutcomeDenied}, Limit: 1},
	)
	if err != nil {
		t.Fatalf("create cursor: %v", err)
	}
	appender.calls = 0
	_, err = service.ListEvents(
		context.Background(), directoryActor(),
		auditing.ListInput{
			Filter: auditing.Filter{Outcome: audit.OutcomeSuccess},
			Limit:  1, Cursor: page.NextCursor,
		},
	)
	if !errors.Is(err, auditing.ErrInvalidCursor) {
		t.Fatalf("expected invalid cursor, got %v", err)
	}
	if appender.calls != 0 {
		t.Fatal("expected invalid request not to be audited")
	}
}

func directoryActor() identity.User {
	return identity.User{
		ID:       "123e4567-e89b-12d3-a456-426614174000",
		Username: "audit_admin", DisplayName: "Audit Administrator",
		Role: identity.RoleAdmin, Active: true,
		CreatedAt: directoryTime(1), UpdatedAt: directoryTime(1),
	}
}

func directoryEvent(id string, hour int) audit.Event {
	return audit.Event{
		ID: id, OccurredAt: directoryTime(hour), Type: audit.TypeMediaContentRead,
		Outcome: audit.OutcomeDenied, TargetType: audit.TargetMedia,
		Reason: audit.ReasonUnknownTarget,
	}
}

func directoryTime(hour int) time.Time {
	return time.Date(2026, time.October, 12, hour, 0, 0, 0, time.UTC)
}
