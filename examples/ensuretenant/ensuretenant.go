package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"

	iamv1beta "buf.build/gen/go/namespace/cloud/protocolbuffers/go/proto/namespace/cloud/iam/v1beta"
	"namespacelabs.dev/integrations/api/iam"
	"namespacelabs.dev/integrations/auth/aws"
)

var (
	identityPool       = flag.String("identity_pool", "", "Identity pool to use.")
	namespacePartnerId = flag.String("partner_id", "", "Partner ID that is federated.")
)

func main() {
	flag.Parse()

	if err := ensure(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func ensure(ctx context.Context) error {
	token, err := aws.Federation(ctx, *identityPool, *namespacePartnerId)
	if err != nil {
		return err
	}

	iam, err := iam.NewClient(ctx, token)
	if err != nil {
		return err
	}

	t, err := iam.Tenants.EnsureTenantForExternalAccount(ctx, &iamv1beta.EnsureTenantForExternalAccountRequest{
		VisibleName:       "test tenant",
		ExternalAccountId: "test-account",
	})
	if err != nil {
		return err
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(t)

	return nil
}
