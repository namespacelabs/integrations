package buildkit

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"

	"buf.build/gen/go/namespace/cloud/grpc/go/proto/namespace/cloud/builder/v1beta/builderv1betagrpc"
	builderv1beta "buf.build/gen/go/namespace/cloud/protocolbuffers/go/proto/namespace/cloud/builder/v1beta"
	bkclient "github.com/moby/buildkit/client"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"namespacelabs.dev/integrations/api"
	"namespacelabs.dev/integrations/api/builds"
	"namespacelabs.dev/integrations/auth/nstls"
)

func Connect(ctx context.Context, builder builderv1betagrpc.BuilderServiceClient) (*bkclient.Client, error) {
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

	return bkclient.New(ctx, resp.FullBuildkitEndpoint, bkclient.WithGRPCDialOption(grpc.WithTransportCredentials(credentials.NewTLS(cfg))))
}

type ConnectOptions struct {
	Client       *builds.Client                         // If none is provided, one is instantiated for this purpose.
	BuildsClient builderv1betagrpc.BuilderServiceClient // If none is provided, one is obtained from the client above.

	Platform string // If not specified, "linux/amd64" is used by default.
}

func ConnectWith(ctx context.Context, token api.TokenAndCertificateSource, options *ConnectOptions) (*bkclient.Client, error) {
	if options == nil {
		options = &ConnectOptions{}
	}

	c := options.BuildsClient
	if c == nil {
		cc := options.Client
		if cc == nil {
			cli, err := builds.NewClient(ctx, token)
			if err != nil {
				return nil, err
			}

			defer cli.Close() // The connection is only used for a single call below.

			cc = &cli
		}

		c = cc.Builder
	}

	platform := options.Platform
	if platform == "" {
		platform = "linux/amd64"
	}

	resp, err := c.GetBuilderConfiguration(ctx, &builderv1beta.GetBuilderConfigurationRequest{
		Platform: platform,
	})
	if err != nil {
		return nil, err
	}

	return bkclient.New(ctx, resp.FullBuildkitEndpoint,
		bkclient.WithGRPCDialOption(grpc.WithTransportCredentials(credentials.NewTLS(
			nstls.ClientConfig(ctx, token),
		))))
}
