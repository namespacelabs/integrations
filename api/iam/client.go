package iam

import (
	"context"

	"google.golang.org/grpc"
	"namespacelabs.dev/integrations/api"
	"namespacelabs.dev/integrations/nsc/apienv"
	"namespacelabs.dev/integrations/nsc/grpcapi"
	iamv1beta "namespacelabs.dev/integrations/proto/namespace/cloud/iam/v1beta"
)

type Client struct {
	Tenants iamv1beta.TenantServiceClient
	Tokens  iamv1beta.TokenServiceClient

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
		Tenants: iamv1beta.NewTenantServiceClient(conn),
		Tokens:  iamv1beta.NewTokenServiceClient(conn),
		Conn:    conn,
	}, nil
}

func (c Client) Close() error {
	return c.Conn.Close()
}
