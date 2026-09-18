package main

import (
	"archive/tar"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

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
	computepb "namespacelabs.dev/integrations/proto/namespace/cloud/compute/v1beta"
)

const serverPort = 8080

var basedir = flag.String("basedir", "", "If not specified, it's computed from the binary's location.")

func main() {
	flag.Parse()

	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	basedir, err := examples.ComputeBaseDir(*basedir)
	if err != nil {
		return err
	}

	token, err := auth.LoadDefaults()
	if err != nil {
		return err
	}

	imageRef, err := buildAndPush(ctx, token, filepath.Join(basedir, "helloworld"))
	if err != nil {
		return err
	}

	cli, err := compute.NewClient(ctx, token)
	if err != nil {
		return err
	}
	defer cli.Close()

	instance, err := cli.Compute.CreateInstance(ctx, &computepb.CreateInstanceRequest{
		Shape: &computepb.InstanceShape{
			VirtualCpu:      6,
			MemoryMegabytes: 14 * 1024,
			Os:              "macos",
			MachineArch:     "arm64",
		},
		DocumentedPurpose: "macOS ingress example",
		Deadline:          timestamppb.New(time.Now().Add(15 * time.Minute)),
		Applications: []*computepb.ApplicationRequest{{
			Name:     "helloworld",
			ImageRef: imageRef,
			Command:  "./entrypoint",
			Args:     []string{"-port", fmt.Sprint(serverPort)},
		}},
	})
	if err != nil {
		return err
	}

	fmt.Printf("Instance created: %s\n", instance.InstanceUrl)

	if _, err := cli.Compute.WaitInstanceSync(ctx, &computepb.WaitInstanceRequest{
		InstanceId: instance.Metadata.InstanceId,
	}); err != nil {
		return err
	}

	ingress, err := cli.Compute.CreateIngress(ctx, &computepb.CreateIngressRequest{
		InstanceId: instance.Metadata.InstanceId,
		Ingresses: []*computepb.IngressRequest{{
			Name: "helloworld",
			HttpMatchRule: []*computepb.HttpMatchRule{{
				DoesNotRequireAuth: true,
			}},
			ExportedPortBackend: &computepb.ExportedPortBackend{
				Port: serverPort,
			},
		}},
	})
	if err != nil {
		return err
	}
	if len(ingress.AllocatedIngresses) != 1 {
		return fmt.Errorf("expected one allocated ingress, got %d", len(ingress.AllocatedIngresses))
	}

	url := "https://" + ingress.AllocatedIngresses[0].Fqdn
	fmt.Printf("Public URL: %s\n", url)

	return verifyEndpoint(ctx, url)
}

func buildAndPush(ctx context.Context, token api.TokenSource, srcdir string) (string, error) {
	dir, err := os.MkdirTemp("", "macingress")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	target := filepath.Join(dir, "entrypoint")
	cmd := exec.CommandContext(ctx, "go", "build", "-o", target, ".")
	cmd.Dir = srcdir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(slices.Clone(os.Environ()), "CGO_ENABLED=0", "GOOS=darwin", "GOARCH=arm64")
	if err := cmd.Run(); err != nil {
		return "", err
	}

	var tarBytes bytes.Buffer
	w := tar.NewWriter(&tarBytes)
	if err := w.AddFS(os.DirFS(dir)); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	image, err := mutate.AppendLayers(empty.Image, stream.NewLayer(io.NopCloser(bytes.NewReader(tarBytes.Bytes()))))
	if err != nil {
		return "", fmt.Errorf("failed to produce image: %w", err)
	}

	repository, err := builds.NSCRImage(ctx, token, "example/macingress/helloworld")
	if err != nil {
		return "", fmt.Errorf("failed to compute repository: %w", err)
	}
	parsed, err := name.NewTag(repository)
	if err != nil {
		return "", fmt.Errorf("failed to parse image ref: %w", err)
	}
	if err := remote.Write(parsed, image, remote.WithContext(ctx), remote.WithAuthFromKeychain(builds.NewNSCRKeychain(token))); err != nil {
		return "", fmt.Errorf("failed to push image: %w", err)
	}

	digest, err := image.Digest()
	if err != nil {
		return "", fmt.Errorf("failed to compute digest: %w", err)
	}

	return parsed.Digest(digest.String()).String(), nil
}

func verifyEndpoint(ctx context.Context, url string) error {
	deadline := time.Now().Add(time.Minute)
	client := &http.Client{Timeout: 5 * time.Second}
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}

		response, err := client.Do(request)
		if err == nil {
			body, readErr := io.ReadAll(response.Body)
			response.Body.Close()
			if readErr == nil && response.StatusCode == http.StatusOK && strings.TrimSpace(string(body)) == "Hello from a Namespace macOS instance!" {
				fmt.Printf("Response: %s", body)
				return nil
			}
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("public endpoint did not become ready within one minute")
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
