package buildkit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth"
	"google.golang.org/grpc"
	"namespacelabs.dev/integrations/api"
)

func NamespaceRegistryAuth(token api.TokenSource) session.Attachable {
	return RegistryAuth(tokenKeychain{token})
}

func RegistryAuth(keychain Keychain) session.Attachable {
	return KeychainWrapper{io.Discard, os.Stderr, keychain, nil}
}

type Keychain interface {
	Resolve(context.Context, string) (*auth.CredentialsResponse, error)
}

type tokenKeychain struct {
	token api.TokenSource
}

func (dk tokenKeychain) Resolve(ctx context.Context, host string) (*auth.CredentialsResponse, error) {
	if host == "nscr.io" {
		token, err := dk.token.IssueToken(ctx, 10*time.Minute, false)
		if err != nil {
			return nil, err
		}

		return &auth.CredentialsResponse{
			Username: "token",
			Secret:   token,
		}, nil
	}

	return nil, nil
}

type KeychainWrapper struct {
	DebugLogger io.Writer
	ErrorLogger io.Writer
	Keychain    Keychain

	Fallback auth.AuthServer
}

func (kw KeychainWrapper) Register(server *grpc.Server) {
	auth.RegisterAuthServer(server, kw)
}

func (kw KeychainWrapper) Credentials(ctx context.Context, req *auth.CredentialsRequest) (*auth.CredentialsResponse, error) {
	response, err := kw.credentials(ctx, req.Host)

	if err == nil {
		fmt.Fprintf(kw.DebugLogger, "[buildkit] AuthServer.Credentials %q --> %q\n", req.Host, response.Username)
	} else {
		fmt.Fprintf(kw.DebugLogger, "[buildkit] AuthServer.Credentials %q: failed: %v\n", req.Host, err)

	}

	return response, err
}

func (kw KeychainWrapper) credentials(ctx context.Context, host string) (*auth.CredentialsResponse, error) {
	authn, err := kw.Keychain.Resolve(ctx, host)
	if err != nil {
		return nil, err
	}

	if authn == nil {
		return &auth.CredentialsResponse{}, nil
	}

	return authn, nil
}

func (kw KeychainWrapper) FetchToken(ctx context.Context, req *auth.FetchTokenRequest) (*auth.FetchTokenResponse, error) {
	if kw.Fallback != nil {
		return kw.Fallback.FetchToken(ctx, req)
	}

	fmt.Fprintf(kw.ErrorLogger, "AuthServer.FetchToken %s\n", asJson(req))
	return nil, fmt.Errorf("unimplemented")
}

func (kw KeychainWrapper) GetTokenAuthority(ctx context.Context, req *auth.GetTokenAuthorityRequest) (*auth.GetTokenAuthorityResponse, error) {
	if kw.Fallback != nil {
		return kw.Fallback.GetTokenAuthority(ctx, req)
	}

	fmt.Fprintf(kw.ErrorLogger, "AuthServer.GetTokenAuthority %s\n", asJson(req))
	return nil, fmt.Errorf("unimplemented")
}

func (kw KeychainWrapper) VerifyTokenAuthority(ctx context.Context, req *auth.VerifyTokenAuthorityRequest) (*auth.VerifyTokenAuthorityResponse, error) {
	if kw.Fallback != nil {
		return kw.Fallback.VerifyTokenAuthority(ctx, req)
	}

	fmt.Fprintf(kw.ErrorLogger, "AuthServer.VerifyTokenAuthority %s\n", asJson(req))
	return nil, fmt.Errorf("unimplemented")
}

func asJson(msg any) string {
	str, _ := json.Marshal(msg)
	return string(str)
}
