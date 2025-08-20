package buildhelper

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	"github.com/moby/buildkit/frontend/dockerui"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/tonistiigi/fsutil"
	"namespacelabs.dev/integrations/api"
	"namespacelabs.dev/integrations/api/builds"
	"namespacelabs.dev/integrations/buildkit"
)

func BuildImageFromDockerfileAndContext(ctx context.Context, debugLog io.Writer, token api.TokenSource, relName, localDir string) (string, error) {
	cli, err := builds.NewClient(ctx, token)
	if err != nil {
		return "", err
	}

	defer cli.Close()

	display, err := progressui.NewDisplay(os.Stdout, progressui.PlainMode)
	if err != nil {
		return "", err
	}

	bk, err := buildkit.Connect(ctx, cli.Builder)
	if err != nil {
		return "", err
	}

	ws, err := fsutil.NewFS(localDir)
	if err != nil {
		return "", err
	}

	target, err := builds.NSCRImage(ctx, token, relName)
	if err != nil {
		return "", err
	}

	nd, err := name.NewTag(target)
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

	return nd.Digest(digest).Name(), nil
}
