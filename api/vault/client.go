package vault

import (
	"context"
	"os"

	"buf.build/gen/go/namespace/cloud/grpc/go/proto/namespace/cloud/vault/v1beta/vaultv1betagrpc"
	"google.golang.org/grpc"
	"namespacelabs.dev/integrations/api"
	"namespacelabs.dev/integrations/nsc/grpcapi"
)

type Client struct {
	Vault vaultv1betagrpc.VaultServiceClient

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
		Vault: vaultv1betagrpc.NewVaultServiceClient(conn),
		Conn:  conn,
	}, nil
}

func (c Client) Close() error {
	return c.Conn.Close()
}
