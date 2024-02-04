package gcpfederation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"namespacelabs.dev/integrations/nsc"
	"namespacelabs.dev/integrations/nsc/apienv"
)

func WithProduceOIDCWorkloadToken(authsrc nsc.TokenSource) func(context.Context, string) (string, error) {
	return func(ctx context.Context, audience string) (string, error) {
		req, err := json.Marshal(map[string]any{
			"audience": audience,
			"version":  1,
		})
		if err != nil {
			return "", err
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", apienv.IAMEndpoint()+"/nsl.tenants.TenantsService/IssueIdToken", bytes.NewReader(req))
		if err != nil {
			return "", err
		}

		bt, err := authsrc.IssueToken(ctx, 30*time.Minute, false)
		if err != nil {
			return "", err
		}

		httpReq.Header.Add("content-type", "application/json")
		httpReq.Header.Add("authorization", "Bearer "+bt)

		log.Printf("Obtaining id_token")

		httpResp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			return "", err
		}

		defer httpResp.Body.Close()

		if httpResp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("got %v", httpResp.Status)
		}

		resp := map[string]any{}
		dec := json.NewDecoder(httpResp.Body)
		if err := dec.Decode(&resp); err != nil {
			return "", err
		}

		if v, ok := resp["id_token"]; ok {
			log.Printf("got id_token")
			return fmt.Sprintf("%v", v), nil
		}

		return "", errors.New("id_token was missing")
	}
}
