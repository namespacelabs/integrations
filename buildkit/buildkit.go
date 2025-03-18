package buildkit

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"

	"buf.build/gen/go/namespace/cloud/grpc/go/proto/namespace/cloud/builder/v1beta/builderv1betagrpc"
	builderv1beta "buf.build/gen/go/namespace/cloud/protocolbuffers/go/proto/namespace/cloud/builder/v1beta"
	"github.com/moby/buildkit/client"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func Connect(ctx context.Context, builder builderv1betagrpc.BuilderServiceClient) (*client.Client, error) {
	resp, err := builder.GetBuilderConfiguration(ctx, &builderv1beta.GetBuilderConfigurationRequest{
		Platform:             "linux/amd64",
		ReturnNewCredentials: true,
	})
	if err != nil {
		return nil, err
	}

	cfg := &tls.Config{}

	if cfg.RootCAs == nil {
		cfg.RootCAs = x509.NewCertPool()
	}

	if resp.ServerCaPem != "" {
		cfg.RootCAs = x509.NewCertPool()

		if ok := cfg.RootCAs.AppendCertsFromPEM([]byte(resp.ServerCaPem)); !ok {
			return nil, errors.New("failed to append ca certs")
		}
	}

	if resp.Credentials == nil {
		return nil, errors.New("credentials are missing")
	}

	cert, err := tls.X509KeyPair([]byte(resp.Credentials.GetClientCertificatePem()), []byte(resp.Credentials.GetPrivateKeyPem()))
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	cfg.Certificates = append(cfg.Certificates, cert)

	return client.New(ctx, resp.FullBuildkitEndpoint, client.WithGRPCDialOption(grpc.WithTransportCredentials(credentials.NewTLS(cfg))))
}
