package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	computepb "buf.build/gen/go/namespace/cloud/protocolbuffers/go/proto/namespace/cloud/compute/v1beta"
	iamv1beta "buf.build/gen/go/namespace/cloud/protocolbuffers/go/proto/namespace/cloud/iam/v1beta"
	"google.golang.org/protobuf/types/known/timestamppb"
	"namespacelabs.dev/integrations/api"
	"namespacelabs.dev/integrations/api/compute"
	"namespacelabs.dev/integrations/api/iam"
	"namespacelabs.dev/integrations/auth"
	"namespacelabs.dev/integrations/auth/aws"
)

var (
	identityPool       = flag.String("identity_pool", "", "Identity pool to use.")
	namespacePartnerId = flag.String("partner_id", "", "Partner ID that is federated.")
	externalAccountId  = flag.String("external_account_id", "", "External ID to identify the tenant to own the instance.")
)

func main() {
	flag.Parse()

	token, err := ensureTenant(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	if err := create(context.Background(), os.Stdout, token, &computepb.InstanceShape{
		VirtualCpu:      2,
		MemoryMegabytes: 4 * 1024,
		MachineArch:     "amd64", // Can also do "arm64".
	}); err != nil {
		log.Fatal(err)
	}
}

func ensureTenant(ctx context.Context) (api.TokenSource, error) {
	token, err := aws.Federation(ctx, *identityPool, *namespacePartnerId)
	if err != nil {
		return nil, err
	}

	iam, err := iam.NewClient(ctx, token)
	if err != nil {
		return nil, err
	}

	tenant, err := iam.Tenants.EnsureTenantForExternalAccount(ctx, &iamv1beta.EnsureTenantForExternalAccountRequest{
		VisibleName:       "test tenant",
		ExternalAccountId: *externalAccountId,
	})
	if err != nil {
		return nil, err
	}

	return auth.TenantTokenSource(iam, tenant.Tenant.Id), nil
}

func create(ctx context.Context, debugLog io.Writer, token api.TokenSource, shape *computepb.InstanceShape) error {
	// Create a stub to use the Namespace Compute API.
	cli, err := compute.NewClient(ctx, token)
	if err != nil {
		return err
	}

	defer cli.Close()

	enc := json.NewEncoder(debugLog)
	enc.SetIndent("", "  ")

	resp, err := cli.Compute.CreateInstance(ctx, &computepb.CreateInstanceRequest{
		Shape:             shape,
		DocumentedPurpose: "createinstance example",
		Deadline:          timestamppb.New(time.Now().Add(1 * time.Hour)),
		// Run the engine in a container.
		Containers: []*computepb.ContainerRequest{{
			Name:     "nginx",
			ImageRef: "nginx",
			Args:     []string{},
			ExportPorts: []*computepb.ContainerPort{
				{Name: "nginx", ContainerPort: 80, Proto: computepb.ContainerPort_TCP},
			},
		}},
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(debugLog, "[namespace] Instance: %s\n", resp.InstanceUrl)

	enc.Encode(resp)

	// Wait until the instance is ready.
	md, err := cli.Compute.WaitInstanceSync(ctx, &computepb.WaitInstanceRequest{
		InstanceId: resp.Metadata.InstanceId,
	})
	if err != nil {
		return err
	}

	_ = enc.Encode(md.Metadata)

	return nil
}
