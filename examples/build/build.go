package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/docker/cli/cli/config"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/frontend/dockerui"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/session/secrets/secretsprovider"
	"namespacelabs.dev/integrations/api/builds"
	"namespacelabs.dev/integrations/auth"
	"namespacelabs.dev/integrations/buildkit"
)

func main() {
	flag.Parse()

	if err := build(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func build(ctx context.Context) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	token, err := auth.LoadUserToken()
	if err != nil {
		return err
	}

	cli, err := builds.NewClient(ctx, token)
	if err != nil {
		return err
	}

	bk, err := buildkit.Connect(ctx, cli.Builder)
	if err != nil {
		return err
	}

	var fs []secretsprovider.Source
	store, err := secretsprovider.NewStore(fs)
	if err != nil {
		return err
	}

	solveOpt := client.SolveOpt{
		Frontend: "dockerfile.v0",
		FrontendInputs: map[string]llb.State{
			dockerui.DefaultLocalNameDockerfile: makeDockerfileState([]byte(`
FROM ubuntu
RUN apt update
RUN apt install -y curl
`)),
			dockerui.DefaultLocalNameContext: makeState(cwd),
		},
	}

	// Scaffolding to support secrets.
	solveOpt.Session = append(solveOpt.Session, secretsprovider.NewSecretProvider(store))

	// Support the caller's docker auth configuration.
	solveOpt.Session = append(solveOpt.Session, authprovider.NewDockerAuthProvider(authprovider.DockerAuthProviderConfig{
		ConfigFile: config.LoadDefaultConfigFile(os.Stderr),
	}))

	ch := make(chan *client.SolveStatus)

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")

	go func() {
		for status := range ch {
			enc.Encode(status)
		}
	}()

	resp, err := bk.Solve(ctx, nil, solveOpt, ch)
	if err != nil {
		return err
	}

	enc.Encode(resp.ExporterResponse)
	return nil
}

func makeDockerfileState(contents []byte) llb.State {
	return llb.Scratch().
		File(llb.Mkfile("/Dockerfile", 0644,
			contents,
			llb.WithCreatedTime(fixedPoint)))
}

func makeState(path string) llb.State {
	return llb.Local(path,
		llb.WithCustomName(fmt.Sprintf("Workspace (from %s)", path)))
}

var fixedPoint = time.UnixMilli(0)
