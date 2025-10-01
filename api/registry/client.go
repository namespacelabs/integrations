package iam

import (
	"context"

	"buf.build/gen/go/namespace/cloud/grpc/go/proto/namespace/cloud/registry/v1beta/registryv1betagrpc"
	"google.golang.org/grpc"
	"namespacelabs.dev/integrations/api"
	"namespacelabs.dev/integrations/nsc/apienv"
	"namespacelabs.dev/integrations/nsc/grpcapi"
)

type Client struct {
	ContainerRegistry registryv1betagrpc.ContainerRegistryServiceClient

	Conn *grpc.ClientConn
}

func NewClient(ctx context.Context, token api.TokenSource, opts ...grpc.DialOption) (Client, error) {
	return NewClientWithEndpoint(ctx, apienv.GlobalEndpoint(), token, opts...)
}

func NewClientWithEndpoint(ctx context.Context, endpoint string, token api.TokenSource, opts ...grpc.DialOption) (Client, error) {
	conn, err := grpcapi.NewConnectionWithEndpoint(ctx, endpoint, token, opts...)
	if err != nil {
		return Client{}, err
	}

	return Client{
		ContainerRegistry: registryv1betagrpc.NewContainerRegistryServiceClient(conn),
		Conn:              conn,
	}, nil
}

func (c Client) Close() error {
	return c.Conn.Close()
}
