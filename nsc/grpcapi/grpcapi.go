package grpcapi

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"namespacelabs.dev/integrations/api"
	"namespacelabs.dev/integrations/nsc"
)

// If set, emits requests and responses to this writer.
// Note: debug writing relies on request interception.
var DebugWriter io.Writer

// If set, shows request payloads in debug output.
var DebugShowRequests bool

// If set, shows response payloads in debug output.
var DebugShowResponses bool

func NewConnectionWithEndpoint(ctx context.Context, endpoint string, token api.TokenSource, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	parsed, err := parseEndpoint(endpoint)
	if err != nil {
		return nil, err
	}

	ourOpts := []grpc.DialOption{
		grpc.WithUserAgent(fmt.Sprintf("nsc-go/%s", nsc.Version)),
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{})),
	}

	if token != nil {
		ourOpts = append(ourOpts, grpc.WithPerRPCCredentials(credWrapper{token}))
	}

	writer := DebugWriter
	if writer == nil && boolean("NS_GRPC_DEBUG") {
		writer = os.Stderr
	}

	debugRequests := DebugShowRequests || boolean("NSC_GRPC_DEBUG_REQUESTS")
	debugResponses := DebugShowResponses || boolean("NSC_GRPC_DEBUG_RESPONSES")

	if writer != nil {
		ourOpts = append(ourOpts, grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			fmt.Fprintf(writer, "[DEBUG] RPC request to: %s [%s]\n", method, endpoint)

			if debugRequests {
				b, _ := json.Marshal(req)
				fmt.Fprintf(writer, "[DEBUG] Payload: %s\n", b)
			}

			err := invoker(ctx, method, req, reply, cc, opts...)
			if err == nil {
				if debugResponses {
					b, _ := json.Marshal(reply)
					fmt.Fprintf(writer, "[DEBUG] Response Payload: %s\n", b)
				}
			} else {
				fmt.Fprintf(writer, "[DEBUG] request failed: %v\n", err)
			}

			return err
		}))
	}

	return grpc.DialContext(ctx, parsed, append(ourOpts, opts...)...)
}

func boolean(env string) bool {
	b, _ := strconv.ParseBool(os.Getenv(env))
	return b
}

func parseEndpoint(endpoint string) (string, error) {
	if strings.HasPrefix(endpoint, "http://") {
		return "", fmt.Errorf("http scheme not supported")
	}

	if strings.HasPrefix(endpoint, "https://") {
		u, err := url.Parse(endpoint)
		if err != nil {
			return "", fmt.Errorf("invalid endpoint: %w", err)
		}

		if strings.TrimPrefix(u.Path, "/") != "" {
			return "", fmt.Errorf("path not supported: %q", u.Path)
		}

		return parseEndpoint(u.Host)
	}

	if strings.IndexByte(endpoint, ':') < 0 {
		return endpoint + ":443", nil
	}

	return endpoint, nil
}

type credWrapper struct {
	token api.TokenSource
}

func (auth credWrapper) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	token, err := auth.token.IssueToken(ctx, 5*time.Minute, false)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"Authorization": "Bearer " + token,
	}, nil
}

func (credWrapper) RequireTransportSecurity() bool { return true }
