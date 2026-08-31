package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	iamv1beta "namespacelabs.dev/integrations/proto/namespace/cloud/iam/v1beta"
	"namespacelabs.dev/integrations/proto/namespace/cloud/iam/v1beta/iamv1betaconnect"
)

func TestTenantCertificateSourceExchangesTenantToken(t *testing.T) {
	tokenSource := &testTokenSource{token: "nsct_test"}
	handler := &tenantCertificateHandler{t: t, expectedToken: tokenSource.token}
	path, serverHandler := iamv1betaconnect.NewTenantServiceHandler(handler)
	server := httptest.NewServer(serverHandler)
	defer server.Close()

	source := tenantCertificateSource{
		tokenSource: tokenSource,
		client: iamv1betaconnect.NewTenantServiceClient(
			server.Client(),
			server.URL,
		),
	}

	minDuration := 15 * time.Minute
	certificate, err := source.IssueCertificate(context.Background(), minDuration, true)
	assert.NoError(t, err)
	assert.Equal(t, minDuration, tokenSource.minDuration)
	assert.True(t, tokenSource.force)
	assert.NotEmpty(t, path)
	assert.Len(t, certificate.Certificate, 1)

	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	assert.NoError(t, err)
	privateKey, ok := certificate.PrivateKey.(*ecdsa.PrivateKey)
	assert.True(t, ok)
	assert.True(t, leaf.PublicKey.(*ecdsa.PublicKey).Equal(&privateKey.PublicKey))
}

type testTokenSource struct {
	token       string
	minDuration time.Duration
	force       bool
}

func (s *testTokenSource) IssueToken(_ context.Context, minDuration time.Duration, force bool) (string, error) {
	s.minDuration = minDuration
	s.force = force
	return s.token, nil
}

type tenantCertificateHandler struct {
	iamv1betaconnect.UnimplementedTenantServiceHandler
	t             *testing.T
	expectedToken string
}

func (h *tenantCertificateHandler) ExchangeTenantTokenForClientCert(_ context.Context, req *connect.Request[iamv1beta.ExchangeTenantTokenForClientCertRequest]) (*connect.Response[iamv1beta.ExchangeTenantTokenForClientCertResponse], error) {
	assert.Equal(h.t, "Bearer "+h.expectedToken, req.Header().Get("Authorization"))
	assert.Empty(h.t, req.Msg.Permissions)

	block, rest := pem.Decode([]byte(req.Msg.PublicKeyPem))
	assert.NotNil(h.t, block)
	assert.Empty(h.t, rest)
	assert.Equal(h.t, "PUBLIC KEY", block.Type)
	publicKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	assert.NoError(h.t, err)
	assert.Equal(h.t, elliptic.P256(), publicKey.(*ecdsa.PublicKey).Curve)

	signer, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	assert.NoError(h.t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "tenant client"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, signer)
	assert.NoError(h.t, err)

	return connect.NewResponse(&iamv1beta.ExchangeTenantTokenForClientCertResponse{
		ClientCertificatePem: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})),
	}), nil
}
