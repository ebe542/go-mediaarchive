package media

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/audit"
	"github.com/ebe542/go-mediaarchive/internal/content"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
	"github.com/google/uuid"
)

var (
	// ErrContentIntegrity indicates inconsistent managed-content metadata or
	// storage state. It must be treated as an internal operational error.
	ErrContentIntegrity = errors.New("managed content integrity failure")
)

// ContentLocationFinder retrieves a managed location after media access has
// been authorized.
type ContentLocationFinder interface {
	FindByMediaID(ctx context.Context, mediaID string) (content.Location, error)
}

// ContentAuditRecorder stores one content event per session, medium, and result.
type ContentAuditRecorder interface {
	AppendOnce(
		ctx context.Context,
		sessionHash [sha256.Size]byte,
		mediaID string,
		outcome audit.Outcome,
		reason audit.Reason,
		recordedAt time.Time,
		event audit.Event,
	) (bool, error)
}

// ReadableContent contains authorized metadata and an open content stream. The
// caller owns Reader and must close it.
type ReadableContent struct {
	Item         domainmedia.Item
	Reader       content.ReadSeekCloser
	Size         int64
	LastModified time.Time
}

// ContentReadService authorizes media access before consulting content
// locations or managed storage.
type ContentReadService struct {
	media     MediaFinder
	grants    GrantFinder
	locations ContentLocationFinder
	store     content.ReadStore
	audit     ContentAuditRecorder
	eventIDs  AuditEventIDGenerator
	clock     Clock
}

// NewContentReadService creates an authorized managed-content reader.
func NewContentReadService(
	media MediaFinder,
	grants GrantFinder,
	locations ContentLocationFinder,
	store content.ReadStore,
	auditRecorder ContentAuditRecorder,
	eventIDGenerator AuditEventIDGenerator,
	clock Clock,
) *ContentReadService {
	return &ContentReadService{
		media:     media,
		grants:    grants,
		locations: locations,
		store:     store,
		audit:     auditRecorder,
		eventIDs:  eventIDGenerator,
		clock:     clock,
	}
}

// Open authorizes read access and opens the corresponding managed content.
func (service *ContentReadService) Open(
	ctx context.Context,
	actor identity.User,
	sessionHash [sha256.Size]byte,
	mediaID string,
) (ReadableContent, error) {
	item, err := authorizeItem(
		ctx,
		actor,
		mediaID,
		domainmedia.PermissionRead,
		service.media,
		service.grants,
	)
	if err != nil {
		return ReadableContent{}, service.recordOpenError(
			ctx,
			actor,
			sessionHash,
			mediaID,
			domainmedia.Item{},
			err,
		)
	}

	location, err := service.locations.FindByMediaID(ctx, item.ID)
	if errors.Is(err, content.ErrLocationNotFound) {
		return ReadableContent{}, service.recordContentEvent(
			ctx, actor, sessionHash, item,
			audit.OutcomeFailure, audit.ReasonMissingContentLocation,
			ErrMediaNotFound,
		)
	}
	if err != nil {
		operationErr := fmt.Errorf("retrieve content location: %w", err)

		return ReadableContent{}, service.recordContentEvent(
			ctx, actor, sessionHash, item,
			audit.OutcomeFailure, audit.ReasonOperationFailure, operationErr,
		)
	}
	if location.MediaID != item.ID {
		operationErr := fmt.Errorf(
			"%w: location belongs to a different medium",
			ErrContentIntegrity,
		)

		return ReadableContent{}, service.recordContentEvent(
			ctx, actor, sessionHash, item,
			audit.OutcomeFailure, audit.ReasonOperationFailure, operationErr,
		)
	}

	opened, err := service.store.Open(ctx, location.StorageKey)
	if errors.Is(err, content.ErrNotFound) {
		return ReadableContent{}, service.recordContentEvent(
			ctx, actor, sessionHash, item,
			audit.OutcomeFailure, audit.ReasonMissingManagedFile,
			ErrMediaNotFound,
		)
	}
	if err != nil {
		operationErr := fmt.Errorf("open managed content: %w", err)

		return ReadableContent{}, service.recordContentEvent(
			ctx, actor, sessionHash, item,
			audit.OutcomeFailure, audit.ReasonOperationFailure, operationErr,
		)
	}
	if opened.Reader == nil {
		operationErr := fmt.Errorf(
			"%w: storage returned no reader",
			ErrContentIntegrity,
		)

		return ReadableContent{}, service.recordContentEvent(
			ctx, actor, sessionHash, item,
			audit.OutcomeFailure, audit.ReasonOperationFailure, operationErr,
		)
	}
	if opened.Size != item.Size {
		integrityErr := fmt.Errorf(
			"%w: stored size does not match media metadata",
			ErrContentIntegrity,
		)
		if closeErr := opened.Reader.Close(); closeErr != nil {
			integrityErr = errors.Join(
				integrityErr,
				fmt.Errorf("close inconsistent managed content: %w", closeErr),
			)
		}

		return ReadableContent{}, service.recordContentEvent(
			ctx, actor, sessionHash, item,
			audit.OutcomeFailure, audit.ReasonOperationFailure, integrityErr,
		)
	}

	return ReadableContent{
		Item:         item,
		Reader:       opened.Reader,
		Size:         opened.Size,
		LastModified: opened.LastModified,
	}, nil
}

// RecordSuccessfulAccess records the first successful access in a login session.
func (service *ContentReadService) RecordSuccessfulAccess(
	ctx context.Context,
	actor identity.User,
	sessionHash [sha256.Size]byte,
	item domainmedia.Item,
) error {
	return service.recordContentEvent(
		ctx, actor, sessionHash, item,
		audit.OutcomeSuccess, "", nil,
	)
}

// RecordOperationalFailure records the first technical failure for the medium.
func (service *ContentReadService) RecordOperationalFailure(
	ctx context.Context,
	actor identity.User,
	sessionHash [sha256.Size]byte,
	item domainmedia.Item,
	operationErr error,
) error {
	return service.recordContentEvent(
		ctx, actor, sessionHash, item,
		audit.OutcomeFailure, audit.ReasonOperationFailure, operationErr,
	)
}

func (service *ContentReadService) recordOpenError(
	ctx context.Context,
	actor identity.User,
	sessionHash [sha256.Size]byte,
	mediaID string,
	item domainmedia.Item,
	operationErr error,
) error {
	if !validContentAuditMediaID(mediaID) {
		return operationErr
	}
	item.ID = mediaID
	if !errors.Is(operationErr, ErrMediaNotFound) {
		return service.recordContentEvent(
			ctx, actor, sessionHash, item,
			audit.OutcomeFailure, audit.ReasonOperationFailure, operationErr,
		)
	}

	return service.recordContentEvent(
		ctx, actor, sessionHash, item,
		audit.OutcomeDenied, audit.ReasonUnknownTarget, operationErr,
	)
}

func (service *ContentReadService) recordContentEvent(
	ctx context.Context,
	actor identity.User,
	sessionHash [sha256.Size]byte,
	item domainmedia.Item,
	outcome audit.Outcome,
	reason audit.Reason,
	operationErr error,
) error {
	now := service.clock().UTC()
	event, err := audit.NewEvent(audit.Event{
		ID:            service.eventIDs(),
		OccurredAt:    now,
		Type:          audit.TypeMediaContentRead,
		Outcome:       outcome,
		ActorID:       actor.ID,
		ActorUsername: actor.Username,
		ActorRole:     string(actor.Role),
		TargetType:    audit.TargetMedia,
		TargetID:      item.ID,
		TargetName:    item.Title,
		Reason:        reason,
	})
	if err != nil {
		return fmt.Errorf("create media content audit event: %w", err)
	}
	if _, err := service.audit.AppendOnce(
		ctx,
		sessionHash,
		item.ID,
		outcome,
		reason,
		now,
		event,
	); err != nil {
		return fmt.Errorf("record media content audit event: %w", err)
	}

	return operationErr
}

func validContentAuditMediaID(mediaID string) bool {
	parsed, err := uuid.Parse(mediaID)

	return err == nil && parsed != uuid.Nil && parsed.String() == mediaID
}
