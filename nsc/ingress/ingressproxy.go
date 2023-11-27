package ingress

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	computev1beta "buf.build/gen/go/namespace/cloud/protocolbuffers/go/proto/namespace/cloud/compute/v1beta"
	"github.com/gorilla/websocket"
	"github.com/jpillora/chisel/share/cnet"
	"namespacelabs.dev/go-ids"
	"namespacelabs.dev/integrations/nsc"
)

func DialEndpoint(ctx context.Context, debugLog io.Writer, token nsc.TokenSource, endpoint string) (net.Conn, error) {
	tid := ids.NewRandomBase32ID(4)
	fmt.Fprintf(debugLog, "[%s] Gateway: dialing %v...\n", tid, endpoint)

	d := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}

	bt, err := token.IssueToken(ctx, 5*time.Minute, false)
	if err != nil {
		return nil, err
	}

	hdrs := http.Header{}
	hdrs.Add("Authorization", "Bearer "+bt)

	t := time.Now()
	wsConn, _, err := d.DialContext(ctx, endpoint, hdrs)
	if err != nil {
		fmt.Fprintf(debugLog, "[%s] Gateway: %v: failed: %v\n", tid, endpoint, err)
		return nil, err
	}

	fmt.Fprintf(debugLog, "[%s] Gateway: dialing %v... took %v\n", tid, endpoint, time.Since(t))

	return cnet.NewWebSocketConn(wsConn), nil
}

func DialHostedService(ctx context.Context, debugLog io.Writer, token nsc.TokenSource, instanceId, ingressDomain, serviceName string, vars url.Values) (net.Conn, error) {
	u := url.URL{
		Scheme:   "wss",
		Host:     fmt.Sprintf("gate.%s", ingressDomain),
		Path:     fmt.Sprintf("/%s/hsvc.%s", instanceId, serviceName),
		RawQuery: vars.Encode(),
	}

	return DialEndpoint(ctx, debugLog, token, u.String())
}

func DialNamedUnixSocket(ctx context.Context, debugLog io.Writer, token nsc.TokenSource, metadata *computev1beta.InstanceMetadata, name string) (net.Conn, error) {
	vars := url.Values{}
	vars.Set("name", name)
	return DialHostedService(ctx, debugLog, token, metadata.InstanceId, metadata.IngressDomain, "unixsocket", vars)
}
