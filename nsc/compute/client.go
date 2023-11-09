package compute

import (
	"context"
	"net/http"
	"os"

	"buf.build/gen/go/namespace/cloud/connectrpc/go/proto/namespace/cloud/compute/v1beta/computev1betaconnect"
	"connectrpc.com/connect"
	"namespacelabs.dev/integrations/nsc/auth"
)

func NewClient(token auth.Token) computev1betaconnect.ComputeServiceClient {
	if endpoint := os.Getenv("NSC_ENDPOINT"); endpoint != "" {
		return NewClientWithEndpoint(endpoint, token)
	}

	return NewClientWithEndpoint("https://eu.compute.namespaceapis.com", token)
}

func NewClientWithEndpoint(endpoint string, token auth.Token) computev1betaconnect.ComputeServiceClient {
	return computev1betaconnect.NewComputeServiceClient(http.DefaultClient, endpoint, connect.WithInterceptors(
		connect.UnaryInterceptorFunc(func(uf connect.UnaryFunc) connect.UnaryFunc {
			return func(ctx context.Context, ar connect.AnyRequest) (connect.AnyResponse, error) {
				ar.Header().Add("Authorization", "Bearer "+token.BearerToken)
				return uf(ctx, ar)
			}
		}),
	))
}
