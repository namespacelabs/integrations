package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	computepb "buf.build/gen/go/namespace/cloud/protocolbuffers/go/proto/namespace/cloud/compute/v1beta"
	cc "github.com/containerd/console"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
	"google.golang.org/protobuf/types/known/timestamppb"
	"namespacelabs.dev/integrations/api"
	"namespacelabs.dev/integrations/api/compute"
	"namespacelabs.dev/integrations/auth"
	"namespacelabs.dev/integrations/buildkit/buildhelper"
	"namespacelabs.dev/integrations/examples"
	"namespacelabs.dev/integrations/nsc/ingress"
)

var (
	basedir  = flag.String("basedir", "", "If not specified, it's computed from the binary's location.")
	optimize = flag.Bool("optimize", false, "Force an optimization pass.")
	shell    = flag.Bool("shell", false, "If true, spawns a shell instead.")
)

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

	// Create a stub to use the Namespace Compute API.
	cli, err := compute.NewClient(ctx, token)
	if err != nil {
		return err
	}

	defer cli.Close()

	builtBase, err := buildAndOptimize(ctx, cli, os.Stderr, token, "test/sidecar/baseimage:v0", filepath.Join(basedir, "main"))
	if err != nil {
		return fmt.Errorf("failed to build main image: %w", err)
	}

	builtSidecar, err := buildAndOptimize(ctx, cli, os.Stderr, token, "test/sidecar/sidecard:v0", filepath.Join(basedir, "sidecard"))
	if err != nil {
		return fmt.Errorf("failed to build sidecar: %w", err)
	}

	return runInstance(ctx, cli, os.Stderr, token, &computepb.InstanceShape{
		VirtualCpu:      4,
		MemoryMegabytes: 8 * 1024,
		Os:              "linux",
		MachineArch:     "amd64",
	}, builtBase, builtSidecar)
}

func buildAndOptimize(ctx context.Context, cli compute.Client, debugLog io.Writer, token api.TokenSource, relName, localDir string) (string, error) {
	built, err := buildhelper.BuildImageFromDockerfileAndContext(ctx, debugLog, token, relName, localDir)
	if err != nil {
		return "", err
	}

	if *optimize {
		// This optimization process happens automatically behind the scenes; but
		// for this example, we force it explicitly.
		progress, err := cli.Compute.OptimizeImage(ctx, &computepb.OptimizeImageRequest{
			ImageRef: built,
		})
		if err != nil {
			return "", fmt.Errorf("failed to call optimizer: %w", err)
		}

		for {
			p, err := progress.Recv()
			if err != nil {
				return "", fmt.Errorf("optimizer failed: %w", err)
			}

			fmt.Fprintf(debugLog, "optimizer: %s\n", p.Status)

			if p.Status == computepb.OptimizeImageProgress_DONE {
				break
			}
		}
	}

	return built, nil
}

func runInstance(ctx context.Context, cli compute.Client, debugLog io.Writer, token api.TokenSource, shape *computepb.InstanceShape, mainImage, sidecardImage string) error {
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

	fmt.Fprintf(debugLog, "[Waiting until instance becomes ready]\n")

	// Wait until the instance is ready.
	md, err := cli.Compute.WaitInstanceSync(ctx, &computepb.WaitInstanceRequest{
		InstanceId: resp.Metadata.InstanceId,
	})
	if err != nil {
		return err
	}

	_ = enc.Encode(md.Metadata)

	return dossh(ctx, cli, token, md.Metadata.InstanceId, resp.Containers[0].Id)
}

func dossh(ctx context.Context, cli compute.Client, token api.TokenSource, instanceId string, containerId string) error {
	// We need to refetch metadata to obtain ssh credentials.
	md, err := cli.Compute.DescribeInstance(ctx, &computepb.DescribeInstanceRequest{
		InstanceId: instanceId,
	})
	if err != nil {
		return fmt.Errorf("failed to describe instance: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(md.ExtendedMetadata.SshMetadata.SshPrivateKey)
	if err != nil {
		return fmt.Errorf("failed to parse private key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: "ctr-id:" + containerId,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.HostKeyCallback(func(hostname string, remote net.Addr, key ssh.PublicKey) error { return nil }),
	}

	conn, err := ingress.DialInstanceService(ctx, io.Discard, token, md.Metadata, "ssh")
	if err != nil {
		return fmt.Errorf("failed to dial ssh: %w", err)
	}

	defer conn.Close()

	c, chans, reqs, err := ssh.NewClientConn(conn, "passthrough", config)
	if err != nil {
		return fmt.Errorf("failed to create ssh connection: %w", err)
	}

	sshcli := ssh.NewClient(c, chans, reqs)
	defer sshcli.Close()

	fmt.Fprintf(os.Stderr, "Connected to %s...\n", instanceId)

	sesh, err := sshcli.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create a ssh session: %w", err)
	}

	defer sesh.Close()

	sesh.Stdout = os.Stdout
	sesh.Stderr = os.Stderr

	if *shell {
		sesh.Stdin = os.Stdin

		localPty, err := cc.ConsoleFromFile(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Could not get console from stdin: %v\n", err)
		} else {
			if err := localPty.SetRaw(); err != nil {
				return err
			}

			defer localPty.Reset()

			w, h, err := term.GetSize(int(localPty.Fd()))
			if err != nil {
				return err
			}

			go listenForResize(ctx, localPty, sesh)

			if err := sesh.RequestPty(os.Getenv("TERM"), h, w, nil); err != nil {
				return err
			}
		}

		if err := sesh.Shell(); err != nil {
			return fmt.Errorf("failed to spawn shell: %w", err)
		}

		return sesh.Wait()
	}

	return sesh.Run("uname -a")
}

func listenForResize(ctx context.Context, stdin cc.Console, session *ssh.Session) {
	sig := make(chan os.Signal, 1)
	notifyWindowSize(sig)

	defer func() {
		signal.Stop(sig)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-sig:
		}

		w, h, err := term.GetSize(int(stdin.Fd()))
		if err == nil {
			session.WindowChange(h, w)
		}
	}
}

func notifyWindowSize(ch chan<- os.Signal) {
	signal.Notify(ch, syscall.SIGWINCH)
}
