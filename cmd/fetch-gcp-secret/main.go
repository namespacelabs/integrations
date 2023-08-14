package main

import (
	"context"
	"flag"
	"log"
	"os"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"google.golang.org/api/option"
	"namespacelabs.dev/integrations/nsc/gcpfederation"
	"namespacelabs.dev/integrations/nsc/localauth"
)

var (
	fetchTo                  = flag.String("to", "", "Where to store the secret.")
	secretName               = flag.String("secret_name", "", "The secret to store.")
	workloadIdentityProvider = flag.String("workload_identity_provider", "", "The workload identity provider.")
	serviceAccount           = flag.String("service_account", "", "The service account we act as.")
)

func main() {
	flag.Parse()

	if *secretName == "" {
		log.Fatal("--secret_name is required")
	}

	if *fetchTo == "" {
		log.Fatal("--to is required")
	}

	if err := do(context.Background(), *secretName, *fetchTo, maybeExchangeOpts()); err != nil {
		log.Fatal(err)
	}
}

func maybeExchangeOpts() *gcpfederation.ExchangeOIDCTokenOpts {
	if *workloadIdentityProvider == "" && *serviceAccount == "" {
		return nil
	}

	if *workloadIdentityProvider == "" {
		log.Fatal("--workload_identity_provider is required")
	}

	if *serviceAccount == "" {
		log.Fatal("--service_account is required")
	}

	return &gcpfederation.ExchangeOIDCTokenOpts{
		WorkloadIdentityProvider: *workloadIdentityProvider,
		ServiceAccount:           *serviceAccount,
	}
}

func do(ctx context.Context, secretName, target string, exchangeopts *gcpfederation.ExchangeOIDCTokenOpts) error {
	var clientopts []option.ClientOption
	if exchangeopts != nil {
		o, err := gcpfederation.SDKOptions(ctx, *exchangeopts, localauth.LoadToken)
		if err != nil {
			return err
		}

		clientopts = o
	}

	sm, err := secretmanager.NewClient(ctx, clientopts...)
	if err != nil {
		return err
	}

	defer sm.Close()

	sec, err := sm.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{
		Name: secretName,
	})
	if err != nil {
		return err
	}

	if err := os.WriteFile(target, sec.GetPayload().GetData(), 0600); err != nil {
		return err
	}

	log.Printf("Wrote %q", target)

	return nil
}
