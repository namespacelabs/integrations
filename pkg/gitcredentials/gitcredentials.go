package gitcredentials

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"namespacelabs.dev/integrations/api"
)

const ObtainGitCredentialsPath = "/nsl.secrets.SecretsService/ObtainGitCredentialsForRepository"

func ParseAttributes(r io.Reader) (map[string]string, error) {
	attributes := map[string]string{}

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			return nil, fmt.Errorf("invalid credential attribute %q", line)
		}
		attributes[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return attributes, nil
}

func RepositoryURL(attributes map[string]string) (string, bool) {
	if attributes["protocol"] != "https" || attributes["host"] == "" || attributes["path"] == "" {
		return "", false
	}

	path := strings.Trim(attributes["path"], "/")
	if path == "" {
		return "", false
	}

	return "https://" + strings.ToLower(attributes["host"]) + "/" + path, true
}

type ObtainGitCredentialsForRepositoryResponse struct {
	Provider  string   `json:"provider,omitempty"`
	URLScope  string   `json:"urlScope,omitempty"`
	Headers   []Header `json:"headers,omitempty"`
	ExpiresAt string   `json:"expiresAt,omitempty"`
}

type Header struct {
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

type Credential struct {
	Username string
	Password string
}

func CredentialFromResponse(repositoryURL string, resp *ObtainGitCredentialsForRepositoryResponse) (*Credential, error) {
	if resp == nil {
		return nil, fmt.Errorf("empty credentials response")
	}

	if len(resp.Headers) == 0 {
		return nil, nil
	}

	if err := requireMatchingScope(repositoryURL, resp.URLScope); err != nil {
		return nil, err
	}

	for _, header := range resp.Headers {
		if !strings.EqualFold(header.Name, "Authorization") || header.Value == "" {
			continue
		}

		scheme, encoded, _ := strings.Cut(header.Value, " ")
		if !strings.EqualFold(scheme, "Basic") {
			return nil, fmt.Errorf("unsupported authorization scheme %q for %q", scheme, repositoryURL)
		}

		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		if err != nil {
			return nil, fmt.Errorf("invalid basic authorization header for %q: %w", repositoryURL, err)
		}

		username, password, found := strings.Cut(string(decoded), ":")
		if !found || username == "" || password == "" {
			return nil, fmt.Errorf("invalid basic authorization credentials for %q", repositoryURL)
		}
		if strings.ContainsAny(username, credentialForbidden) || strings.ContainsAny(password, credentialForbidden) {
			return nil, fmt.Errorf("invalid characters in credentials for %q", repositoryURL)
		}
		return &Credential{Username: username, Password: password}, nil
	}

	return nil, fmt.Errorf("credentials response for %q has no Authorization header", repositoryURL)
}

const credentialForbidden = "\r\n\x00"

func requireMatchingScope(repositoryURL, urlScope string) error {
	if urlScope == "" {
		return fmt.Errorf("credentials response for %q has no URL scope", repositoryURL)
	}

	scopeURL, err := url.Parse(urlScope)
	if err != nil || scopeURL.Scheme != "https" || scopeURL.Hostname() == "" {
		return fmt.Errorf("credentials scope %q is not a valid https URL prefix", urlScope)
	}

	repoURL, err := url.Parse(repositoryURL)
	if err != nil {
		return fmt.Errorf("invalid repository URL %q: %w", repositoryURL, err)
	}

	if !strings.EqualFold(scopeURL.Scheme, repoURL.Scheme) || !strings.EqualFold(scopeURL.Host, repoURL.Host) {
		return fmt.Errorf("credentials scope %q does not cover repository %q", urlScope, repositoryURL)
	}

	scopePath := strings.TrimSuffix(scopeURL.Path, "/")
	if scopeURL.RawQuery != "" || scopeURL.Fragment != "" ||
		(repoURL.Path != scopePath && !strings.HasPrefix(repoURL.Path, scopePath+"/")) {
		return fmt.Errorf("credentials scope %q does not cover repository %q", urlScope, repositoryURL)
	}

	return nil
}

func Fetch(ctx context.Context, endpoint string, tokenSource api.TokenSource, repositoryURL string, httpClient *http.Client) (*ObtainGitCredentialsForRepositoryResponse, error) {
	bearer, err := tokenSource.IssueToken(ctx, 5*time.Minute, false)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(map[string]string{"repository_url": repositoryURL})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)

	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if msg := resp.Header.Get("grpc-message"); msg != "" {
			return nil, fmt.Errorf("%s: %s", resp.Status, msg)
		}

		output, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("failed with status: %v\n%s", resp.Status, output)
	}

	credentials := &ObtainGitCredentialsForRepositoryResponse{}
	if err := json.NewDecoder(resp.Body).Decode(credentials); err != nil {
		return nil, err
	}

	return credentials, nil
}

type Options struct {
	Endpoint    string
	TokenSource api.TokenSource
	HTTPClient  *http.Client
	Debug       io.Writer
}

func Resolve(ctx context.Context, opts Options, repositoryURL string) (*Credential, *ObtainGitCredentialsForRepositoryResponse, error) {
	ctx, done := context.WithTimeout(ctx, 10*time.Second)
	defer done()

	resp, err := Fetch(ctx, opts.Endpoint, opts.TokenSource, repositoryURL, opts.HTTPClient)
	if err != nil {
		return nil, nil, err
	}

	credential, err := CredentialFromResponse(repositoryURL, resp)
	if err != nil {
		return nil, resp, err
	}

	return credential, resp, nil
}

func Get(ctx context.Context, opts Options, in io.Reader, out io.Writer) error {
	attributes, err := ParseAttributes(in)
	if err != nil {
		return err
	}

	repositoryURL, ok := RepositoryURL(attributes)
	if !ok {
		debugf(opts.Debug, "not a valid HTTPS repository request; emitting no credentials")
		return nil
	}

	debugf(opts.Debug, "resolving credentials for %q", repositoryURL)

	credential, resp, err := Resolve(ctx, opts, repositoryURL)
	if err != nil {
		return err
	}

	debugf(opts.Debug, "resolved provider %q with URL scope %q", resp.Provider, resp.URLScope)

	if credential == nil {
		debugf(opts.Debug, "repository does not require credentials; emitting none")
		return nil
	}

	fmt.Fprintf(out, "username=%s\n", credential.Username)
	fmt.Fprintf(out, "password=%s\n", credential.Password)
	fmt.Fprintln(out)
	return nil
}

func debugf(w io.Writer, format string, args ...any) {
	if w != nil {
		fmt.Fprintf(w, format+"\n", args...)
	}
}
