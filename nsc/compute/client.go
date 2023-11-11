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
	"namespacelabs.dev/integrations/nsc/auth"
)

type Client struct {
	Compute computev1betagrpc.ComputeServiceClient
	Storage computev1betagrpc.StorageServiceClient
	Usage   computev1betagrpc.UsageServiceClient

	Conn *grpc.ClientConn
}

func NewClient(ctx context.Context, token auth.Token, opts ...grpc.DialOption) (Client, error) {
	if endpoint := os.Getenv("NSC_ENDPOINT"); endpoint != "" {
		return NewClientWithEndpoint(ctx, endpoint, token)
	}

	return NewClientWithEndpoint(ctx, "https://eu.compute.namespaceapis.com", token, opts...)
}

func NewClientWithEndpoint(ctx context.Context, endpoint string, token auth.Token, opts ...grpc.DialOption) (Client, error) {
	parsed, err := parseEndpoint(endpoint)
	if err != nil {
		return Client{}, err
	}

	conn, err := grpc.DialContext(ctx, parsed,
		append([]grpc.DialOption{
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
	token auth.Token
}

func (auth credWrapper) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	return map[string]string{
		"Authorization": "Bearer " + auth.token.BearerToken,
	}, nil
}

func (credWrapper) RequireTransportSecurity() bool { return true }
