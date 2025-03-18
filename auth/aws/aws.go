package aws

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentity"
	"namespacelabs.dev/integrations/api"
)

func Federation(ctx context.Context, identityPool, namespacePartnerId string) (api.TokenSource, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS sdk: %w", err)
	}

	return FederationFromConfig(cfg, identityPool, namespacePartnerId), nil
}

func FederationFromConfig(config aws.Config, identityPool, namespacePartnerId string) api.TokenSource {
	return cognitoTokens{cognitoidentity.NewFromConfig(config), identityPool, namespacePartnerId}
}

type cognitoTokens struct {
	client             *cognitoidentity.Client
	identityPool       string
	namespacePartnerId string
}

func (c cognitoTokens) IssueToken(ctx context.Context, minDuration time.Duration, force bool) (string, error) {
	params := &cognitoidentity.GetOpenIdTokenForDeveloperIdentityInput{
		IdentityPoolId: aws.String(c.identityPool),
		Logins: map[string]string{
			"namespace.so": c.namespacePartnerId,
		},
	}

	resp, err := c.client.GetOpenIdTokenForDeveloperIdentity(ctx, params)
	if err != nil {
		return "", fmt.Errorf("failed to get Cognito token: %w", err)
	}

	return "cognito_" + *resp.Token, nil
}
