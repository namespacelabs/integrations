package iam

import (
	"context"

	"buf.build/gen/go/namespace/cloud/grpc/go/proto/namespace/cloud/iam/v1beta/iamv1betagrpc"
	"google.golang.org/grpc"
	"namespacelabs.dev/integrations/api"
	"namespacelabs.dev/integrations/nsc/apienv"
	"namespacelabs.dev/integrations/nsc/grpcapi"
)

type Client struct {
	Tenants iamv1betagrpc.TenantServiceClient
	Tokens  iamv1betagrpc.TokenServiceClient

	Conn *grpc.ClientConn
}

func NewClient(ctx context.Context, token api.TokenSource, opts ...grpc.DialOption) (Client, error) {
	return NewClientWithEndpoint(ctx, apienv.IAMEndpoint(), token, opts...)
}

func NewClientWithEndpoint(ctx context.Context, endpoint string, token api.TokenSource, opts ...grpc.DialOption) (Client, error) {
	conn, err := grpcapi.NewConnectionWithEndpoint(ctx, endpoint, token, opts...)
	if err != nil {
		return Client{}, err
	}

	return Client{
		Tenants: iamv1betagrpc.NewTenantServiceClient(conn),
		Tokens:  iamv1betagrpc.NewTokenServiceClient(conn),
		Conn:    conn,
	}, nil
}

func (c Client) Close() error {
	return c.Conn.Close()
}
