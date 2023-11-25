package nsc

import "context"

type TokenSource interface {
	IssueToken(context.Context) (string, error)
}
