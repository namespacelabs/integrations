package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"

	"namespacelabs.dev/integrations/auth"
	"namespacelabs.dev/integrations/nsc/apienv"
	"namespacelabs.dev/integrations/pkg/gitcredentials"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("git-credential-nsc", flag.ContinueOnError)
	flags.SetOutput(stderr)

	var (
		repository = flags.String("repository", "", "The repository URL to fetch credentials for (used with --validate).")
		validate   = flags.Bool("validate", false, "If true, validates that credentials can be obtained for --repository.")
		debug      = flags.Bool("debug", false, "Whether to emit debug statements.")
	)
	if err := flags.Parse(args); err != nil {
		return 2
	}

	endpoint := apienv.IAMEndpoint() + gitcredentials.ObtainGitCredentialsPath

	if *validate {
		if *repository == "" {
			fmt.Fprintln(stderr, "--repository is required with --validate")
			return 2
		}

		if err := validateCredentials(context.Background(), endpoint, *repository, stdout); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}

		return 0
	}

	if flags.Arg(0) != "get" {
		return 0
	}

	tokenSource, err := auth.LoadDefaults()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	if err := gitcredentials.Get(context.Background(), gitcredentials.Options{
		Endpoint:    endpoint,
		TokenSource: tokenSource,
		Debug:       debugWriter(stderr, *debug),
	}, stdin, stdout); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	return 0
}

func validateCredentials(ctx context.Context, endpoint, repository string, stdout io.Writer) error {
	parsed, err := url.Parse(repository)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.Path == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("invalid repository URL")
	}

	tokenSource, err := auth.LoadDefaults()
	if err != nil {
		return err
	}

	credential, resp, err := gitcredentials.Resolve(ctx, gitcredentials.Options{
		Endpoint:    endpoint,
		TokenSource: tokenSource,
	}, repository)
	if err != nil {
		return err
	}

	if credential == nil {
		fmt.Fprintf(stdout, "%s: no credentials returned; the repository may be public or have no configured association. Authenticated access was not verified (provider: %q)\n", repository, resp.Provider)
		return nil
	}

	fmt.Fprintf(stdout, "%s: obtained %s credentials, valid until %s\n", repository, resp.Provider, resp.ExpiresAt)
	return nil
}

func debugWriter(stderr io.Writer, debug bool) io.Writer {
	if debug {
		return stderr
	}
	return nil
}
