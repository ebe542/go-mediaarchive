package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/audit"
	"github.com/ebe542/go-mediaarchive/internal/content"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

var (
	// ErrInvalidMaximumUploadSize indicates a non-positive service size limit.
	ErrInvalidMaximumUploadSize = errors.New("invalid maximum upload size")

	// ErrUploadCompensationFailed indicates that failed upload persistence also
	// left newly stored content requiring operational reconciliation.
	ErrUploadCompensationFailed = errors.New("upload compensation failed")
)

// ManagedCreator atomically persists a media identity and content location.
type ManagedCreator interface {
	CreateManagedWithAudit(
		ctx context.Context,
		item domainmedia.Item,
		location content.Location,
		event audit.Event,
	) error
}

// AuditEventIDGenerator creates identifiers for immutable upload events.
type AuditEventIDGenerator func() string

// UploadInput contains caller-controlled metadata and a streaming source.
type UploadInput struct {
	Title            string
	Authors          []string
	OriginalFilename string
	Type             domainmedia.Type
	MIMEType         string
	Source           io.Reader
}

// UploadService coordinates managed content and atomic metadata creation.
type UploadService struct {
	repository  ManagedCreator
	store       content.Store
	generateID  IDGenerator
	audit       audit.Appender
	eventIDs    AuditEventIDGenerator
	clock       Clock
	maximumSize int64
}

// NewUploadService creates a managed-content upload service.
func NewUploadService(
	repository ManagedCreator,
	store content.Store,
	idGenerator IDGenerator,
	auditAppender audit.Appender,
	eventIDGenerator AuditEventIDGenerator,
	clock Clock,
	maximumSize int64,
) (*UploadService, error) {
	if maximumSize <= 0 {
		return nil, ErrInvalidMaximumUploadSize
	}

	return &UploadService{
		repository:  repository,
		store:       store,
		generateID:  idGenerator,
		audit:       auditAppender,
		eventIDs:    eventIDGenerator,
		clock:       clock,
		maximumSize: maximumSize,
	}, nil
}

// UploadItem stores a file and atomically persists its derived metadata and
// location. A database or validation failure removes the newly stored file.
func (service *UploadService) UploadItem(
	ctx context.Context,
	actor identity.User,
	input UploadInput,
) (domainmedia.Item, error) {
	now := service.clock()
	if !mayCreateMedia(actor) {
		return domainmedia.Item{}, service.recordUploadEvent(
			ctx,
			actor,
			"",
			"",
			audit.OutcomeDenied,
			audit.ReasonInsufficientRole,
			now,
			ErrCreationForbidden,
		)
	}

	mediaID := service.generateID()
	stored, err := service.store.Put(
		ctx,
		mediaID,
		input.Source,
		service.maximumSize,
	)
	if err != nil {
		return domainmedia.Item{}, fmt.Errorf("store uploaded content: %w", err)
	}

	item, err := domainmedia.NewItem(
		mediaID,
		input.Title,
		input.Authors,
		input.OriginalFilename,
		input.Type,
		input.MIMEType,
		stored.Size,
		stored.Checksum[:],
		actor.ID,
		now,
		now,
	)
	if err != nil {
		operationErr := service.compensateStoredContent(
			ctx,
			actor,
			mediaID,
			stored.StorageKey,
			fmt.Errorf("create uploaded media identity: %w", err),
		)
		if errors.Is(operationErr, ErrUploadCompensationFailed) {
			return domainmedia.Item{}, operationErr
		}

		return domainmedia.Item{}, service.recordUploadEvent(
			ctx,
			actor,
			mediaID,
			"",
			audit.OutcomeDenied,
			audit.ReasonInvalidInput,
			now,
			operationErr,
		)
	}

	location, err := content.NewLocation(item.ID, stored.StorageKey, now)
	if err != nil {
		return domainmedia.Item{}, service.compensateStoredContent(
			ctx,
			actor,
			item.ID,
			stored.StorageKey,
			fmt.Errorf("create uploaded content location: %w", err),
		)
	}
	event, err := service.newUploadEvent(
		actor,
		item.ID,
		item.Title,
		audit.OutcomeSuccess,
		"",
		now,
	)
	if err != nil {
		return domainmedia.Item{}, service.compensateStoredContent(
			ctx,
			actor,
			item.ID,
			stored.StorageKey,
			err,
		)
	}
	if err := service.repository.CreateManagedWithAudit(
		ctx,
		item,
		location,
		event,
	); err != nil {
		return domainmedia.Item{}, service.compensateStoredContent(
			ctx,
			actor,
			item.ID,
			stored.StorageKey,
			fmt.Errorf("persist uploaded media: %w", err),
		)
	}

	return item, nil
}

func (service *UploadService) compensateStoredContent(
	ctx context.Context,
	actor identity.User,
	mediaID string,
	storageKey string,
	cause error,
) error {
	// Cleanup must still be attempted when the request context was canceled
	// after the content store completed its write.
	if err := service.store.Delete(
		context.WithoutCancel(ctx),
		storageKey,
	); err != nil {
		operationErr := errors.Join(
			ErrUploadCompensationFailed,
			cause,
			fmt.Errorf("remove stored content after failure: %w", err),
		)

		return service.recordUploadEvent(
			context.WithoutCancel(ctx),
			actor,
			mediaID,
			"",
			audit.OutcomeFailure,
			audit.ReasonOperationFailure,
			service.clock(),
			operationErr,
		)
	}

	return cause
}

func (service *UploadService) recordUploadEvent(
	ctx context.Context,
	actor identity.User,
	mediaID string,
	title string,
	outcome audit.Outcome,
	reason audit.Reason,
	now time.Time,
	operationErr error,
) error {
	event, err := service.newUploadEvent(
		actor,
		mediaID,
		title,
		outcome,
		reason,
		now,
	)
	if err != nil {
		return err
	}
	if err := service.audit.Append(ctx, event); err != nil {
		return fmt.Errorf("record media upload event: %w", err)
	}

	return operationErr
}

func (service *UploadService) newUploadEvent(
	actor identity.User,
	mediaID string,
	title string,
	outcome audit.Outcome,
	reason audit.Reason,
	now time.Time,
) (audit.Event, error) {
	event, err := audit.NewEvent(audit.Event{
		ID:            service.eventIDs(),
		OccurredAt:    now.UTC(),
		Type:          audit.TypeMediaUploaded,
		Outcome:       outcome,
		ActorID:       actor.ID,
		ActorUsername: actor.Username,
		ActorRole:     string(actor.Role),
		TargetType:    audit.TargetMedia,
		TargetID:      mediaID,
		TargetName:    title,
		Reason:        reason,
	})
	if err != nil {
		return audit.Event{}, fmt.Errorf("create media upload audit event: %w", err)
	}

	return event, nil
}
