package api

import (
	"context"
	"crypto/tls"
	"time"
)

type TokenSource interface {
	// IssueToken receives the minimum duration it should be active for. If
	// force is true, skip any token caches available.
	//
	// Returns a token used as a "Bearer token".
	IssueToken(ctx context.Context, minDuration time.Duration, force bool) (string, error)
}

type CertificateSource interface {
	// IssueCertificate receives the minimum duration it should be active for. If
	// force is true, skip any token caches available.
	//
	// Returns a public and private key certificate which can be used to authenticate as a tenant.
	IssueCertificate(ctx context.Context, minDuration time.Duration, force bool) (tls.Certificate, error)
}

type TokenAndCertificateSource interface {
	TokenSource
	CertificateSource
}
