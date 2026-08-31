package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"namespacelabs.dev/integrations/api"
	"namespacelabs.dev/integrations/api/iam"
	"namespacelabs.dev/integrations/nsc/apienv"
	iamv1beta "namespacelabs.dev/integrations/proto/namespace/cloud/iam/v1beta"
	"namespacelabs.dev/integrations/proto/namespace/cloud/iam/v1beta/iamv1betaconnect"
)

func TenantTokenSource(client iam.Client, tenantId string) api.TokenAndCertificateSource {
	return iamTokenSource{client, tenantId}
}

func TenantCertificateSource(tokenSource api.TokenSource) api.CertificateSource {
	return tenantCertificateSource{
		tokenSource: tokenSource,
		client: iamv1betaconnect.NewTenantServiceClient(
			http.DefaultClient,
			apienv.IAMEndpoint(),
		),
	}
}

type tenantCertificateSource struct {
	tokenSource api.TokenSource
	client      iamv1betaconnect.TenantServiceClient
}

func (ts tenantCertificateSource) IssueCertificate(ctx context.Context, minDuration time.Duration, force bool) (tls.Certificate, error) {
	tenantToken, err := ts.tokenSource.IssueToken(ctx, minDuration, force)
	if err != nil {
		return tls.Certificate{}, err
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	publicKeyDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	publicKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicKeyDER})

	req := connect.NewRequest(&iamv1beta.ExchangeTenantTokenForClientCertRequest{
		PublicKeyPem: string(publicKeyPEM),
	})
	req.Header().Set("Authorization", "Bearer "+tenantToken)

	resp, err := ts.client.ExchangeTenantTokenForClientCert(ctx, req)
	if err != nil {
		return tls.Certificate{}, err
	}
	if resp.Msg.GetClientCertificatePem() == "" {
		return tls.Certificate{}, fmt.Errorf("tenant client certificate response missing certificate")
	}

	privateKeyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privateKeyDER})

	return tls.X509KeyPair([]byte(resp.Msg.GetClientCertificatePem()), privateKeyPEM)
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
