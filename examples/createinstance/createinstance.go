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
	"google.golang.org/protobuf/types/known/timestamppb"
	"namespacelabs.dev/integrations/api"
	"namespacelabs.dev/integrations/api/compute"
	"namespacelabs.dev/integrations/auth"
)

func main() {
	flag.Parse()

	token, err := auth.LoadDefaults()
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

func create(ctx context.Context, debugLog io.Writer, token api.TokenSource, shape *computepb.InstanceShape) error {
	// Create a stub to use the Namespace Compute API.
	cli, err := compute.NewClient(ctx, token)
	if err != nil {
		return err
	}

	defer cli.Close()

	// Create or re-use an existing instance that runs the dagger engine.
	resp, err := cli.Compute.CreateInstance(ctx, &computepb.CreateInstanceRequest{
		Shape:             shape,
		DocumentedPurpose: "createinstance example",
		// Block until resources for the instance have been allocated.
		Interactive: true,
		Deadline:    timestamppb.New(time.Now().Add(1 * time.Hour)),
		// Run the engine in a container.
		Containers: []*computepb.ContainerRequest{{
			Name:     "nginx",
			ImageRef: "nginx",
			Args:     []string{},
		}},
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(debugLog, "[namespace] Instance: %s\n", resp.InstanceUrl)

	// Wait until the instance is ready.
	md, err := cli.Compute.WaitInstanceSync(ctx, &computepb.WaitInstanceRequest{
		InstanceId: resp.Metadata.InstanceId,
	})
	if err != nil {
		return err
	}

	enc := json.NewEncoder(debugLog)
	enc.SetIndent("", "  ")
	return enc.Encode(md.Metadata)
}
