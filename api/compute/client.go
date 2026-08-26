package compute

import (
	"context"
	"os"

	"google.golang.org/grpc"
	"namespacelabs.dev/integrations/api"
	"namespacelabs.dev/integrations/nsc/grpcapi"
	computev1beta "namespacelabs.dev/integrations/proto/namespace/cloud/compute/v1beta"
)

type Client struct {
	Compute       computev1beta.ComputeServiceClient
	Storage       computev1beta.StorageServiceClient
	Usage         computev1beta.UsageServiceClient
	Observability computev1beta.ObservabilityServiceClient

	Conn *grpc.ClientConn
}

func NewClient(ctx context.Context, token api.TokenSource, opts ...grpc.DialOption) (Client, error) {
	if endpoint := os.Getenv("NSC_ENDPOINT"); endpoint != "" {
		return NewClientWithEndpoint(ctx, endpoint, token)
	}

	return NewClientWithEndpoint(ctx, "https://us.compute.namespaceapis.com", token, opts...)
}

func NewClientWithEndpoint(ctx context.Context, endpoint string, token api.TokenSource, opts ...grpc.DialOption) (Client, error) {
	conn, err := grpcapi.NewConnectionWithEndpoint(ctx, endpoint, token, opts...)
	if err != nil {
		return Client{}, err
	}

	return Client{
		Compute:       computev1beta.NewComputeServiceClient(conn),
		Storage:       computev1beta.NewStorageServiceClient(conn),
		Usage:         computev1beta.NewUsageServiceClient(conn),
		Observability: computev1beta.NewObservabilityServiceClient(conn),
		Conn:          conn,
	}, nil
}

func (c Client) Close() error {
	return c.Conn.Close()
}
