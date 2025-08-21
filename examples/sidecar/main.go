package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	computepb "buf.build/gen/go/namespace/cloud/protocolbuffers/go/proto/namespace/cloud/compute/v1beta"
	"google.golang.org/protobuf/types/known/timestamppb"
	"namespacelabs.dev/integrations/api"
	"namespacelabs.dev/integrations/api/compute"
	"namespacelabs.dev/integrations/auth"
	"namespacelabs.dev/integrations/buildkit/buildhelper"
	"namespacelabs.dev/integrations/examples"
)

var basedir = flag.String("basedir", "", "If not specified, it's computed from the binary's location.")

func main() {
	flag.Parse()

	if err := do(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func do(ctx context.Context) error {
	basedir, err := examples.ComputeBaseDir(*basedir)
	if err != nil {
		return err
	}

	token, err := auth.LoadDefaults()
	if err != nil {
		return err
	}

	builtBase, err := buildhelper.BuildImageFromDockerfileAndContext(ctx, os.Stderr, token, "test/sidecar/baseimage", filepath.Join(basedir, "main"))
	if err != nil {
		return fmt.Errorf("failed to build main image: %w", err)
	}

	builtSidecar, err := buildhelper.BuildImageFromDockerfileAndContext(ctx, os.Stderr, token, "test/sidecar/sidecard", filepath.Join(basedir, "sidecard"))
	if err != nil {
		return fmt.Errorf("failed to build sidecar: %w", err)
	}

	return runInstance(ctx, os.Stderr, token, &computepb.InstanceShape{
		VirtualCpu:      4,
		MemoryMegabytes: 8 * 1024,
		Os:              "linux",
		MachineArch:     "amd64",
	}, builtBase, builtSidecar)
}

func runInstance(ctx context.Context, debugLog io.Writer, token api.TokenSource, shape *computepb.InstanceShape, mainImage, sidecardImage string) error {
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
			Name:     "testsidecar",
			ImageRef: mainImage,
			Args: []string{
				"/sidecar/entrypoint",
				"-cmd", "sleep 180000",
			},
			DockerSockPath: "/var/run/docker.sock",          // Enable docker.
			Network:        computepb.ContainerRequest_HOST, // Enable access to docker.
			Experimental: &computepb.ContainerRequest_ExperimentalFeatures{
				SidecarVolumes: []*computepb.ContainerRequest_ExperimentalFeatures_SidecarVolume{
					{Name: "sidecar", ImageRef: sidecardImage, ContainerPath: "/sidecar"},
				},
				IncrementalLoading: true,
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
