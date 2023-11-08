package ingressproxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jpillora/chisel/share/cnet"
	"namespacelabs.dev/go-ids"
	"namespacelabs.dev/integrations/nsc/localauth"
)

func DialEndpoint(ctx context.Context, debugLog io.Writer, token localauth.TokenJson, endpoint string) (net.Conn, error) {
	tid := ids.NewRandomBase32ID(4)
	fmt.Fprintf(debugLog, "[%s] Gateway: dialing %v...\n", tid, endpoint)

	d := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}

	hdrs := http.Header{}
	hdrs.Add("Authorization", "Bearer "+token.BearerToken)

	t := time.Now()
	wsConn, _, err := d.DialContext(ctx, endpoint, hdrs)
	if err != nil {
		fmt.Fprintf(debugLog, "[%s] Gateway: %v: failed: %v\n", tid, endpoint, err)
		return nil, err
	}

	fmt.Fprintf(debugLog, "[%s] Gateway: dialing %v... took %v\n", tid, endpoint, time.Since(t))

	return cnet.NewWebSocketConn(wsConn), nil
}

func DialHostedService(ctx context.Context, debugLog io.Writer, token localauth.TokenJson, instanceId, ingressDomain, serviceName string, vars url.Values) (net.Conn, error) {
	u := url.URL{
		Scheme:   "wss",
		Host:     fmt.Sprintf("gate.%s", ingressDomain),
		Path:     fmt.Sprintf("/%s/hsvc.%s", instanceId, serviceName),
		RawQuery: vars.Encode(),
	}

	return DialEndpoint(ctx, debugLog, token, u.String())
}
