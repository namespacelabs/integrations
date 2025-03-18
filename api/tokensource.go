package api

import (
	"context"
	"time"
)

type TokenSource interface {
	// IssueToken receives the minimum duration it should be active for. If
	// force is true, skip any token caches available.
	//
	// Returns a token used as a "Bearer token".
	IssueToken(ctx context.Context, minDuration time.Duration, force bool) (string, error)
}
