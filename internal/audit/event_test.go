package audit_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/audit"
)

const (
	eventID = "123e4567-e89b-12d3-a456-426614174000"
	actorID = "223e4567-e89b-12d3-a456-426614174000"
)

func TestNewEventAcceptsAuthenticatedAndAnonymousEvents(t *testing.T) {
	authenticated := validEvent()
	stored, err := audit.NewEvent(authenticated)
	if err != nil {
		t.Fatalf("create authenticated event: %v", err)
	}
	if stored.Range == authenticated.Range {
		t.Fatal("expected audit event to own its range value")
	}

	anonymous := validEvent()
	anonymous.Type = audit.TypeSessionCreateDenied
	anonymous.Outcome = audit.OutcomeDenied
	anonymous.ActorID = ""
	anonymous.ActorUsername = ""
	anonymous.ActorRole = ""
	anonymous.TargetType = audit.TargetUser
	anonymous.TargetName = "requested-user"
	anonymous.Reason = audit.ReasonInvalidCredentials
	anonymous.Range = nil
	if _, err := audit.NewEvent(anonymous); err != nil {
		t.Fatalf("create anonymous event: %v", err)
	}
}

func TestNewEventRejectsInvalidFields(t *testing.T) {
	testCases := []struct {
		name        string
		change      func(*audit.Event)
		expectedErr error
	}{
		{"event ID", func(event *audit.Event) { event.ID = "not-a-uuid" }, audit.ErrInvalidEventID},
		{"timestamp", func(event *audit.Event) { event.OccurredAt = time.Now() }, audit.ErrInvalidTimestamp},
		{"type", func(event *audit.Event) { event.Type = "custom.event" }, audit.ErrInvalidType},
		{"outcome", func(event *audit.Event) { event.Outcome = "unknown" }, audit.ErrInvalidOutcome},
		{"actor ID", func(event *audit.Event) { event.ActorID = "invalid" }, audit.ErrInvalidActor},
		{"anonymous snapshot", func(event *audit.Event) { event.ActorID = "" }, audit.ErrInvalidActor},
		{"actor role", func(event *audit.Event) { event.ActorRole = "owner" }, audit.ErrInvalidActor},
		{"target type", func(event *audit.Event) { event.TargetType = "file" }, audit.ErrInvalidTarget},
		{"target text", func(event *audit.Event) { event.TargetName = "unsafe\nname" }, audit.ErrInvalidTarget},
		{"target ID size", func(event *audit.Event) { event.TargetID = strings.Repeat("a", 201) }, audit.ErrInvalidTarget},
		{"reason", func(event *audit.Event) { event.Reason = "raw_error" }, audit.ErrInvalidReason},
		{"success reason", func(event *audit.Event) { event.Reason = audit.ReasonOperationFailure }, audit.ErrInvalidReason},
		{"range start", func(event *audit.Event) { event.Range.Start = -1 }, audit.ErrInvalidRange},
		{"range order", func(event *audit.Event) { event.Range.End = 1000 }, audit.ErrInvalidRange},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			event := validEvent()
			testCase.change(&event)
			if _, err := audit.NewEvent(event); !errors.Is(err, testCase.expectedErr) {
				t.Fatalf("expected %v, got %v", testCase.expectedErr, err)
			}
		})
	}
}

func TestAuditCodesRemainClosed(t *testing.T) {
	if audit.Type("custom").Valid() {
		t.Error("expected custom event type to be rejected")
	}
	if audit.Outcome("custom").Valid() {
		t.Error("expected custom outcome to be rejected")
	}
	if audit.TargetType("custom").Valid() {
		t.Error("expected custom target type to be rejected")
	}
	if audit.Reason("custom").Valid() {
		t.Error("expected custom reason to be rejected")
	}
}

func validEvent() audit.Event {
	return audit.Event{
		ID:            eventID,
		OccurredAt:    time.Date(2026, time.October, 4, 9, 30, 0, 0, time.UTC),
		Type:          audit.TypeMediaContentRead,
		Outcome:       audit.OutcomeSuccess,
		ActorID:       actorID,
		ActorUsername: "reader",
		ActorRole:     "viewer",
		TargetType:    audit.TargetMedia,
		TargetID:      "323e4567-e89b-12d3-a456-426614174000",
		TargetName:    "Security Handbook",
		Range: &audit.ByteRange{
			Start: 0,
			End:   499,
			Total: 1000,
		},
	}
}
