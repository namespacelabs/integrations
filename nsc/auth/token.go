package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"buf.build/gen/go/namespace/cloud/grpc/go/proto/namespace/private/sessions/sessionsv1betagrpc"
	sessions "buf.build/gen/go/namespace/cloud/protocolbuffers/go/proto/namespace/private/sessions"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/durationpb"
	"namespacelabs.dev/integrations/nsc"
	"namespacelabs.dev/integrations/nsc/grpcapi"
)

type loadedToken struct {
	BearerToken  string
	SessionToken string

	dir      string
	debugLog io.Writer

	mu             sync.Mutex
	sessionsClient sessionsv1betagrpc.UserSessionsServiceClient
}

func (t *loadedToken) client(ctx context.Context) (sessionsv1betagrpc.UserSessionsServiceClient, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.sessionsClient == nil {
		conn, err := grpcapi.NewConnectionWithEndpoint(ctx, iamEndpoint(), nil)
		if err != nil {
			return nil, err
		}

		t.sessionsClient = sessionsv1betagrpc.NewUserSessionsServiceClient(conn)
	}

	return t.sessionsClient, nil
}

func iamEndpoint() string {
	if v := os.Getenv("NSC_IAM_ENDPOINT"); v != "" {
		return v
	}

	return "https://api.namespacelabs.net"
}

func (t *loadedToken) IssueToken(ctx context.Context, minDur time.Duration, skipCache bool) (string, error) {
	if t.SessionToken != "" {
		issue := func(dur time.Duration) (string, error) {
			cli, err := t.client(ctx)
			if err != nil {
				return "", err
			}

			res, err := cli.IssueTenantTokenFromSession(setGrpcBearer(ctx, t.SessionToken), &sessions.IssueTenantTokenFromSessionRequest{
				TokenDuration: durationpb.New(dur),
			})
			if err != nil {
				return "", err
			}

			return res.TenantToken, nil
		}

		if skipCache {
			return issue(minDur)
		}

		sessionClaims, err := tokenClaims(t.debugLog, t.SessionToken)
		if err != nil {
			return "", err
		}

		if t.dir != "" {
			cachePath := filepath.Join(t.dir, "token.cache")
			cacheContents, err := os.ReadFile(cachePath)
			if err != nil {
				if !os.IsNotExist(err) {
					return "", err
				}
			} else {
				cacheClaims, err := tokenClaims(t.debugLog, string(cacheContents))
				if err != nil {
					return "", err
				}

				if cacheClaims.TenantID == sessionClaims.TenantID {
					if cacheClaims.VerifyExpiresAt(time.Now().Add(minDur), true) {
						return string(cacheContents), nil
					}
				}
			}
		}

		dur := 2 * minDur
		if dur > time.Hour {
			dur = time.Hour
		}

		newToken, err := issue(dur)
		if err == nil && t.dir != "" {
			cachePath := filepath.Join(t.dir, "token.cache")
			if err := os.WriteFile(cachePath, []byte(newToken), 0600); err != nil {
				fmt.Fprintf(t.debugLog, "Failed to write token cache: %v\n", err)
			}
		}

		return newToken, err
	}

	return t.BearerToken, nil
}

func LoadDefaults() (nsc.TokenSource, error) {
	if tf := os.Getenv("NSC_TOKEN_FILE"); tf != "" {
		return loadFromFile(tf)
	}

	t, err := loadFromFile("/var/run/nsc/token.json")
	if err == nil {
		return t, nil
	}

	if !os.IsNotExist(err) {
		return nil, err
	}

	return LoadUserToken()
}

func LoadUserToken() (nsc.TokenSource, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}

	token, err := loadFromFile(filepath.Join(dir, "token.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("you are not logged in to Namespace; try running `nsc login`")
		}
	}

	return token, err
}

func LoadWorkloadToken() (nsc.TokenSource, error) {
	if tf := os.Getenv("NSC_TOKEN_FILE"); tf != "" {
		return loadFromFile(tf)
	}

	return loadFromFile("/var/run/nsc/token.json")
}

func loadFromFile(tokenFile string) (nsc.TokenSource, error) {
	contents, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, err
	}

	var tj struct {
		BearerToken  string `json:"bearer_token"`
		SessionToken string `json:"session_token"`
	}

	if err := json.Unmarshal(contents, &tj); err != nil {
		return nil, fmt.Errorf("failed to parse token file: %w", err)
	}

	return &loadedToken{
		BearerToken:  tj.BearerToken,
		SessionToken: tj.SessionToken,
		dir:          filepath.Dir(tokenFile),
		debugLog:     io.Discard,
	}, nil
}

func configDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return dir, err
	}

	return filepath.Join(dir, "ns"), nil
}

func setGrpcBearer(ctx context.Context, bearer string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", fmt.Sprintf("Bearer %s", bearer))
}
