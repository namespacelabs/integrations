package gcpfederation

import (
	"bytes"
	"context"

	"google.golang.org/api/option"
	"namespacelabs.dev/integrations/nsc/localauth"
)

func SDKOptions(ctx context.Context, opts ExchangeOIDCTokenOpts, gt func() (localauth.TokenJson, error)) ([]option.ClientOption, error) {
	t, err := gt()
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := GenerateGcloudSdkConfiguration(ctx, &buf, WithProduceOIDCWorkloadToken(t.BearerToken), opts); err != nil {
		return nil, err
	}

	return []option.ClientOption{option.WithCredentialsJSON(buf.Bytes())}, nil
}
