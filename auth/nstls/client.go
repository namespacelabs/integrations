package nstls

import (
	"context"
	"crypto/tls"
	"time"

	"namespacelabs.dev/integrations/api"
)

// ClientConfig provides a TLS configuration that can be used for mutual
// authenticated communication with Namespace services, and customer instances.
//
// It issues certificates on demand, and does so within the context passed in.
// If that context gets canceled, subsequent certification issuing will also
// fail.
func ClientConfig(ctx context.Context, token api.CertificateSource) *tls.Config {
	return &tls.Config{
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			cert, err := token.IssueCertificate(ctx, 15*time.Minute, false)
			if err != nil {
				return nil, err
			}

			return &cert, nil
		},
	}
}
