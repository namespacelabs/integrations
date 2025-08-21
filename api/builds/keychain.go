package builds

import (
	"context"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"namespacelabs.dev/integrations/api"
)

func NewNSCRKeychain(src api.TokenSource) authn.Keychain {
	return dyn{
		m: map[string]api.TokenSource{
			"nscr.io":         src,
			"staging.nscr.io": src,
			"testing.nscr.io": src,
		},
	}
}

type dyn struct {
	m map[string]api.TokenSource
}

func (d dyn) Resolve(resource authn.Resource) (authn.Authenticator, error) {
	return d.ResolveContext(context.Background(), resource)
}

func (d dyn) ResolveContext(ctx context.Context, resource authn.Resource) (authn.Authenticator, error) {
	if x, ok := d.m[resource.RegistryStr()]; ok {
		return tokenSourceAuthenticator{x}, nil
	}

	return authn.Anonymous, nil
}

type tokenSourceAuthenticator struct {
	src api.TokenSource
}

func (ts tokenSourceAuthenticator) Authorization() (*authn.AuthConfig, error) {
	return ts.AuthorizationContext(context.Background())
}

func (ts tokenSourceAuthenticator) AuthorizationContext(ctx context.Context) (*authn.AuthConfig, error) {
	tok, err := ts.src.IssueToken(ctx, time.Hour, false)
	if err != nil {
		return nil, err
	}

	return &authn.AuthConfig{
		Username: "token",
		Password: tok,
	}, nil
}
