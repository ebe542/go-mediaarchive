package audit

import "context"

// Appender persists immutable audit events.
type Appender interface {
	Append(ctx context.Context, event Event) error
}
