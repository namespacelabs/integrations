package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	computepb "buf.build/gen/go/namespace/cloud/protocolbuffers/go/proto/namespace/cloud/compute/v1beta"
	iamv1beta "buf.build/gen/go/namespace/cloud/protocolbuffers/go/proto/namespace/cloud/iam/v1beta"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	"github.com/moby/buildkit/frontend/dockerui"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/tonistiigi/fsutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/timestamppb"
	"namespacelabs.dev/integrations/api"
	"namespacelabs.dev/integrations/api/compute"
	"namespacelabs.dev/integrations/api/iam"
	"namespacelabs.dev/integrations/auth"
	"namespacelabs.dev/integrations/auth/aws"
	"namespacelabs.dev/integrations/auth/nstls"
	"namespacelabs.dev/integrations/buildkit"
	"namespacelabs.dev/integrations/examples/buildandrun/testserver/proto"
)

var (
	server             = flag.String("server_path", "", "Path of the server to build.")
	identityPool       = flag.String("identity_pool", "", "Identity pool to use.")
	namespacePartnerId = flag.String("partner_id", "", "Partner ID that is federated.")
)

func main() {
	flag.Parse()

	if *server == "" {
		log.Fatal("-server_path is required")
	}

	if err := do(context.Background(), os.Stderr); err != nil {
		log.Fatal(err)
	}
}

func do(ctx context.Context, debugLog io.Writer) error {
	// We use partner account AWS federation in this example.
	federation, err := aws.Federation(ctx, *identityPool, *namespacePartnerId)
	if err != nil {
		return fmt.Errorf("failed to configure aws federation: %w", err)
	}

	// First we'll create a tenant to work with.
	tenant, tenanttoken, err := ensureTenant(ctx, debugLog, federation)
	if err != nil {
		return fmt.Errorf("failed to created tenant: %w", err)
	}

	imageRef, err := buildImage(ctx, debugLog, tenant, tenanttoken)
	if err != nil {
		return fmt.Errorf("failed to build image: %w", err)
	}

	fqdn, err := createInstance(ctx, debugLog, tenanttoken, &computepb.InstanceShape{
		VirtualCpu:      2,
		MemoryMegabytes: 4 * 1024,
		MachineArch:     "amd64",
		Os:              "linux",
	}, imageRef)
	if err != nil {
		return err
	}

	if err := callInstance(ctx, debugLog, tenanttoken, fqdn); err != nil {
		return fmt.Errorf("failed to call instance: %w", err)
	}

	return nil
}

func ensureTenant(ctx context.Context, debugLog io.Writer, token api.TokenSource) (*iamv1beta.Tenant, api.TokenAndCertificateSource, error) {
	iam, err := iam.NewClient(ctx, token)
	if err != nil {
		return nil, nil, err
	}

	resp, err := iam.Tenants.EnsureTenantForExternalAccount(ctx, &iamv1beta.EnsureTenantForExternalAccountRequest{
		VisibleName:       "test tenant",
		ExternalAccountId: "test-account",
	})
	if err != nil {
		return nil, nil, err
	}

	fmt.Fprintf(debugLog, "Tenant: %s\n", resp.Tenant.Id)

	return resp.Tenant, auth.TenantTokenSource(iam, resp.Tenant.Id), nil
}

func buildImage(ctx context.Context, debugLog io.Writer, tenant *iamv1beta.Tenant, token api.TokenAndCertificateSource) (string, error) {
	display, err := progressui.NewDisplay(os.Stdout, progressui.PlainMode)
	if err != nil {
		return "", err
	}

	bk, err := buildkit.ConnectWith(ctx, token, nil)
	if err != nil {
		return "", err
	}

	fmt.Fprintf(debugLog, "building image at %s...\n", *server)

	// XXX missing API.
	target := fmt.Sprintf("nscr.io/%s/test", strings.TrimPrefix(tenant.Id, "tenant_"))

	ws, err := fsutil.NewFS(*server)
	if err != nil {
		return "", err
	}

	solveOpt := client.SolveOpt{
		Frontend: "dockerfile.v0",
		Exports: []client.ExportEntry{{
			Type: client.ExporterImage,
			Attrs: map[string]string{
				"push":              "true",
				"name":              target,
				"push-by-digest":    "true",
				"buildinfo":         "false", // Remove build info to keep reproducibility.
				"source-date-epoch": "0",
			},
		}},

		FrontendInputs: map[string]llb.State{
			dockerui.DefaultLocalNameDockerfile: llb.Local("workspace"),
			dockerui.DefaultLocalNameContext:    llb.Local("workspace"),
		},

		LocalMounts: map[string]fsutil.FS{
			"workspace": ws,
		},
	}

	solveOpt.Session = append(solveOpt.Session, buildkit.NamespaceRegistryAuth(token))

	ch := make(chan *client.SolveStatus)

	go func() {
		_, _ = display.UpdateFrom(ctx, ch)
	}()

	resp, err := bk.Solve(ctx, nil, solveOpt, ch)
	if err != nil {
		return "", err
	}

	digest := resp.ExporterResponse[exptypes.ExporterImageDigestKey]
	if digest == "" {
		return "", fmt.Errorf("digest missing from the output")
	}

	output := target + "@" + digest
	fmt.Fprintf(debugLog, "image built: %s\n", output)

	return output, nil
}

func createInstance(ctx context.Context, debugLog io.Writer, token api.TokenSource, shape *computepb.InstanceShape, imageRef string) (string, error) {
	cli, err := compute.NewClient(ctx, token)
	if err != nil {
		return "", err
	}

	defer cli.Close()

	resp, err := cli.Compute.CreateInstance(ctx, &computepb.CreateInstanceRequest{
		Shape:             shape,
		DocumentedPurpose: "createinstance example",
		Deadline:          timestamppb.New(time.Now().Add(5 * time.Minute)),
		Containers: []*computepb.ContainerRequest{{
			Name:     "test",
			ImageRef: imageRef,
			Args:     []string{},
			ExportPorts: []*computepb.ContainerPort{
				{Name: "grpc", ContainerPort: 15000, Proto: computepb.ContainerPort_TCP},
			},
		}},
	})
	if err != nil {
		return "", err
	}

	var endpoint string
	for _, ctr := range resp.Containers {
		for _, port := range ctr.ExportedPort {
			endpoint = port.Endpoint
			fmt.Fprintf(debugLog, " %d -> %s\n", port.ContainerPort, port.Endpoint)
		}
	}

	fmt.Fprintf(debugLog, "Created instance: %s (waiting until it's ready)\n", resp.InstanceUrl)

	if _, err := cli.Compute.WaitInstanceSync(ctx, &computepb.WaitInstanceRequest{
		InstanceId: resp.Metadata.InstanceId,
	}); err != nil {
		return "", err
	}

	fmt.Fprintf(debugLog, "Instance ready.\n")

	return endpoint, nil
}

func callInstance(ctx context.Context, debugLog io.Writer, token api.CertificateSource, target string) error {
	conn, err := grpc.NewClient(target,
		grpc.WithTransportCredentials(credentials.NewTLS(nstls.ClientConfig(ctx, token))))
	if err != nil {
		return err
	}

	resp, err := proto.NewTestServiceClient(conn).Echo(ctx, &proto.EchoRequest{
		Request: "hello world",
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(debugLog, "received: %q\n", resp.Reply)

	return nil
}
