package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"namespacelabs.dev/integrations/nsc/auth"
)

var (
	repository = flag.String("repository", "", "The repository to fetch credentials for.")
	secretID   = flag.String("secret_id", "", "The secret that represents the association.")
	generate   = flag.Bool("generate", false, "If true, emits a token immediately.")
	validate   = flag.Bool("validate", false, "If true, validates that the arguments are usable.")
	debug      = flag.Bool("debug", false, "Whether to emit debug statements.")
)

func main() {
	flag.Parse()

	switch {
	case *validate:
		if !strings.HasPrefix(*repository, "https://github.com/") {
			fmt.Fprintf(os.Stderr, "Using GitHub short-term credentials requires HTTPS access (saw %q).\n", *repository)
			os.Exit(2)
		}

	case *generate:
		if err := gen(context.Background(), *repository, *secretID); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}

	default:
		if err := helper(context.Background(), *repository, *secretID); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
	}

	os.Exit(0)
}

func gen(ctx context.Context, repository, secretID string) error {
	token, err := auth.LoadWorkloadToken()
	if err != nil {
		return err
	}

	tok, err := fetch(context.Background(), token, repository, secretID)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "%s\n", tok)
	return nil
}

func helper(ctx context.Context, repository, secretID string) error {
	if repository == "" || secretID == "" {
		return errors.New("--repository and --secret_id are required")
	}

	token, err := auth.LoadWorkloadToken()
	if err != nil {
		return err
	}

	switch flag.Arg(0) {
	case "get":
		scanner := bufio.NewScanner(os.Stdin)
		attributes := map[string]string{}
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				break
			}

			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				attributes[parts[0]] = parts[1]
			}
		}

		if *debug {
			fmt.Fprintf(os.Stderr, "Obtained the following attributes: %v\n", attributes)
		}

		if attributes["protocol"] == "https" && attributes["host"] == "github.com" {
			requestForRepository := repository

			if attributes["path"] != "" {
				// "path" will be {org}/{repository}.git
				requestForRepository = attributes["path"]

				if *debug {
					fmt.Fprintf(os.Stderr, "Will request permissions for %q instead.\n", requestForRepository)
				}
			}

			ctx, done := context.WithTimeout(ctx, 10*time.Second)
			defer done()

			tok, err := fetch(ctx, token, requestForRepository, secretID)
			if err != nil {
				return err
			}

			fmt.Fprintf(os.Stdout, "username=token\n")
			fmt.Fprintf(os.Stdout, "password=%s\n", tok)
			// (don’t forget the blank line at the end; it tells git credential that the application finished feeding all the information it has)
			fmt.Fprintln(os.Stdout)
		}
	}

	return nil
}

func fetch(ctx context.Context, token auth.Token, repository, secretID string) (string, error) {
	request := map[string]string{
		"repository": repository,
		"secret_id":  secretID,
	}

	if *debug {
		fmt.Fprintf(os.Stderr, "Will fetch credentials with the following request: %v\n", request)
	}

	bodyBytes, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.namespacelabs.net/nsl.secrets.SecretsService/ObtainGitHubCredentials", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token.BearerToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if msg := resp.Header.Get("grpc-message"); msg != "" {
			return "", errors.New(msg)
		}

		output, _ := io.ReadAll(resp.Body)

		return "", fmt.Errorf("failed with status: %v\n%s", resp.Status, output)
	}

	dec := json.NewDecoder(resp.Body)

	var r ObtainGitHubCredentialsResponse
	if err := dec.Decode(&r); err != nil {
		return "", err
	}

	return r.Token, nil
}

type ObtainGitHubCredentialsResponse struct {
	Token string `json:"token,omitempty"`
}
