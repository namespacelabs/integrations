package nsc

import (
	"context"
	"time"
)

type TokenSource interface {
	IssueToken(context.Context, time.Duration, bool) (string, error)
}
