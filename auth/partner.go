package auth

import (
	"context"
	"crypto/tls"
	"time"

	iamv1beta "buf.build/gen/go/namespace/cloud/protocolbuffers/go/proto/namespace/cloud/iam/v1beta"
	"namespacelabs.dev/integrations/api"
	"namespacelabs.dev/integrations/api/iam"
)

func TenantTokenSource(client iam.Client, tenantId string) api.TokenAndCertificateSource {
	return iamTokenSource{client, tenantId}
}

func TenantCertificateSource(client iam.Client, tenantId string) api.TokenAndCertificateSource {
	return iamTokenSource{client, tenantId}
}

type iamTokenSource struct {
	client   iam.Client
	tenantId string
}

func (ts iamTokenSource) IssueToken(ctx context.Context, minDuration time.Duration, force bool) (string, error) {
	// TODO implement token caching.
	token, err := ts.client.Tenants.IssueTenantToken(ctx, &iamv1beta.IssueTenantTokenRequest{
		TenantId:     ts.tenantId,
		DurationSecs: int64(minDuration.Seconds()),
	})
	if err != nil {
		return "", err
	}

	return token.BearerToken, nil
}

func (ts iamTokenSource) IssueCertificate(ctx context.Context, minDuration time.Duration, force bool) (tls.Certificate, error) {
	// TODO implement certificate caching.
	resp, err := ts.client.Tenants.IssueTenantClientCertificate(ctx, &iamv1beta.IssueTenantClientCertificateRequest{
		TenantId:     ts.tenantId,
		DurationSecs: int64(minDuration.Seconds()),
	})
	if err != nil {
		return tls.Certificate{}, err
	}

	return tls.X509KeyPair([]byte(resp.ClientCertificatePem), []byte(resp.PrivateKeyPem))
}
