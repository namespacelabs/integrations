package compute

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"os"
	"strings"

	"buf.build/gen/go/namespace/cloud/grpc/go/proto/namespace/cloud/compute/v1beta/computev1betagrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"namespacelabs.dev/integrations/nsc"
)

type Client struct {
	Compute computev1betagrpc.ComputeServiceClient
	Storage computev1betagrpc.StorageServiceClient
	Usage   computev1betagrpc.UsageServiceClient

	Conn *grpc.ClientConn
}

func NewClient(ctx context.Context, token nsc.TokenSource, opts ...grpc.DialOption) (Client, error) {
	if endpoint := os.Getenv("NSC_ENDPOINT"); endpoint != "" {
		return NewClientWithEndpoint(ctx, endpoint, token)
	}

	return NewClientWithEndpoint(ctx, "https://eu.compute.namespaceapis.com", token, opts...)
}

func NewClientWithEndpoint(ctx context.Context, endpoint string, token nsc.TokenSource, opts ...grpc.DialOption) (Client, error) {
	parsed, err := parseEndpoint(endpoint)
	if err != nil {
		return Client{}, err
	}

	conn, err := grpc.DialContext(ctx, parsed,
		append([]grpc.DialOption{
			grpc.WithUserAgent(fmt.Sprintf("nsc-go/%s", nsc.Version)),
			grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{})),
			grpc.WithPerRPCCredentials(credWrapper{token}),
		}, opts...)...,
	)
	if err != nil {
		return Client{}, err
	}

	return Client{
		Compute: computev1betagrpc.NewComputeServiceClient(conn),
		Storage: computev1betagrpc.NewStorageServiceClient(conn),
		Usage:   computev1betagrpc.NewUsageServiceClient(conn),
		Conn:    conn,
	}, nil
}

func parseEndpoint(endpoint string) (string, error) {
	if strings.HasPrefix(endpoint, "http://") {
		return "", fmt.Errorf("http scheme not supported")
	}

	if strings.HasPrefix(endpoint, "https://") {
		u, err := url.Parse(endpoint)
		if err != nil {
			return "", fmt.Errorf("invalid endpoint: %w", err)
		}

		if strings.TrimPrefix(u.Path, "/") != "" {
			return "", fmt.Errorf("path not supported: %q", u.Path)
		}

		return parseEndpoint(u.Host)
	}

	if strings.IndexByte(endpoint, ':') < 0 {
		return endpoint + ":443", nil
	}

	return endpoint, nil
}

type credWrapper struct {
	token nsc.TokenSource
}

func (auth credWrapper) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	token, err := auth.token.IssueToken(ctx)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"Authorization": "Bearer " + token,
	}, nil
}

func (credWrapper) RequireTransportSecurity() bool { return true }
