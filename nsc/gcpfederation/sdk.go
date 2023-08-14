package gcpfederation

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
)

type ResolveJWT func(ctx context.Context, audience string) (string, error)

type ExchangeOIDCTokenOpts struct {
	WorkloadIdentityProvider string
	ServiceAccount           string
}

func GenerateGcloudSdkConfiguration(ctx context.Context, out io.Writer, resolve ResolveJWT, opts ExchangeOIDCTokenOpts) error {
	// Facilitate copy-paste.
	identityProvider := strings.TrimPrefix(opts.WorkloadIdentityProvider, "https://iam.googleapis.com/")

	token, err := resolve(ctx, "https://iam.googleapis.com/"+identityProvider)
	if err != nil {
		return err
	}

	f, err := os.CreateTemp("", "idtoken")
	if err != nil {
		return err
	}

	if _, err := f.Write([]byte(token)); err != nil {
		return err
	}

	if err := f.Close(); err != nil {
		return err
	}

	// Write a gcloud compatible configuration.
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{
		"type":               "external_account",
		"audience":           "//iam.googleapis.com/" + identityProvider,
		"subject_token_type": "urn:ietf:params:oauth:token-type:jwt",
		"token_url":          "https://sts.googleapis.com/v1/token",
		"credential_source": map[string]any{
			"file": f.Name(),
		},
		"service_account_impersonation_url": "https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/" + opts.ServiceAccount + ":generateAccessToken",
		"service_account_impersonation": map[string]any{
			"token_lifetime_seconds": 600,
		},
	})
}
