package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"time"

	computepb "buf.build/gen/go/namespace/cloud/protocolbuffers/go/proto/namespace/cloud/compute/v1beta"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/stream"
	"google.golang.org/protobuf/types/known/timestamppb"
	"namespacelabs.dev/integrations/api"
	"namespacelabs.dev/integrations/api/builds"
	"namespacelabs.dev/integrations/api/compute"
	"namespacelabs.dev/integrations/auth"
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

	dir, err := os.MkdirTemp("", "go")
	if err != nil {
		return err
	}

	defer os.RemoveAll(dir)

	target := filepath.Join(dir, "entrypoint")
	if err := gobuild(ctx, target, filepath.Join(basedir, "helloworld")); err != nil {
		return err
	}

	var tarBytes bytes.Buffer

	// Make a tar with the go binary we built.
	w := tar.NewWriter(&tarBytes)
	if err := w.AddFS(os.DirFS(dir)); err != nil {
		return err
	}

	if err := w.Close(); err != nil {
		return err
	}

	newImage, err := mutate.AppendLayers(empty.Image, stream.NewLayer(io.NopCloser(bytes.NewReader(tarBytes.Bytes()))))
	if err != nil {
		return fmt.Errorf("failed to produce image: %w", err)
	}

	x, err := builds.NSCRImage(ctx, token, "example/macrun/helloworld")
	if err != nil {
		return fmt.Errorf("failed to compute repository: %w", err)
	}

	parsed, err := name.NewTag(x)
	if err != nil {
		return fmt.Errorf("failed to parse image ref? %w", err)
	}

	if err := remote.Write(parsed, newImage, remote.WithContext(ctx), remote.WithAuthFromKeychain(builds.NewNSCRKeychain(token))); err != nil {
		return fmt.Errorf("failed to push image: %w", err)
	}

	digest, err := newImage.Digest()
	if err != nil {
		return fmt.Errorf("failed to compute digest: %w", err)
	}

	// Pushed image will have exactly the computed digest.
	imageRef := parsed.Digest(digest.String()).String()

	return runInstance(ctx, os.Stderr, token, &computepb.InstanceShape{
		VirtualCpu:      6,
		MemoryMegabytes: 14 * 1024,
		Os:              "macos",
		MachineArch:     "arm64",
	}, imageRef)
}

func gobuild(ctx context.Context, target, srcdir string) error {
	fmt.Fprintf(os.Stderr, "Running: go build -v -o %s .\n", target)

	cmd := exec.CommandContext(ctx, "go", "build", "-v", "-o", target, ".")
	cmd.Dir = srcdir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Build a darwin/arm64 binary.
	cmd.Env = append(slices.Clone(os.Environ()), "CGO_ENABLED=0", "GOOS=darwin", "GOARCH=arm64")
	return cmd.Run()
}

func runInstance(ctx context.Context, debugLog io.Writer, token api.TokenSource, shape *computepb.InstanceShape, mainImage string) error {
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
		Applications: []*computepb.ApplicationRequest{{
			Name:     "helloworld",
			ImageRef: mainImage,
			Command:  "entrypoint",
			Args: []string{
				"-what", "caller",
			},
		}},
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(debugLog, "[namespace] Instance created: %s\n", resp.InstanceUrl)

	enc.Encode(resp)

	fmt.Fprintf(debugLog, "[Waiting until instance becomes ready]\n")

	// Wait until the instance is ready.
	md, err := cli.Compute.WaitInstanceSync(ctx, &computepb.WaitInstanceRequest{
		InstanceId: resp.Metadata.InstanceId,
	})
	if err != nil {
		return err
	}

	_ = enc.Encode(md.Metadata)

	fmt.Fprintf(debugLog, "[namespace] Instance ready: %s\n", resp.InstanceUrl)

	return nil
}
