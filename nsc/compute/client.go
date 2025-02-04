package compute

import (
	"context"
	"os"

	"buf.build/gen/go/namespace/cloud/grpc/go/proto/namespace/cloud/compute/v1beta/computev1betagrpc"
	"google.golang.org/grpc"
	"namespacelabs.dev/integrations/nsc"
	"namespacelabs.dev/integrations/nsc/grpcapi"
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
	conn, err := grpcapi.NewConnectionWithEndpoint(ctx, endpoint, token, opts...)
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

func (c Client) Close() error {
	return c.Conn.Close()
}
