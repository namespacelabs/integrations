package gitcredentials

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"namespacelabs.dev/integrations/api"
)

func TestParseAttributes(t *testing.T) {
	input := "protocol=https\nhost=github.com\npath=namespacelabs/internal.git\n\nignored-after-blank=1"
	attributes, err := ParseAttributes(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseAttributes: %v", err)
	}

	want := map[string]string{"protocol": "https", "host": "github.com", "path": "namespacelabs/internal.git"}
	for key, value := range want {
		if attributes[key] != value {
			t.Errorf("attributes[%q] = %q, want %q", key, attributes[key], value)
		}
	}
	if _, ok := attributes["ignored-after-blank"]; ok {
		t.Errorf("attributes parsed past the terminating blank line: %v", attributes)
	}
}

func TestParseAttributesPreservesProtocolBytes(t *testing.T) {
	attributes, err := ParseAttributes(strings.NewReader("protocol=https\npath= org/repo.git \nwwwauth[]=challenge"))
	if err != nil {
		t.Fatalf("ParseAttributes: %v", err)
	}

	if attributes["path"] != " org/repo.git " {
		t.Errorf(`attributes["path"] = %q, want %q`, attributes["path"], " org/repo.git ")
	}
	if attributes["wwwauth[]"] != "challenge" {
		t.Errorf(`attributes["wwwauth[]"] = %q, want "challenge"`, attributes["wwwauth[]"])
	}
}

func TestParseAttributesInvalidLine(t *testing.T) {
	if _, err := ParseAttributes(strings.NewReader("protocolhttps\n")); err == nil {
		t.Errorf("expected error for attribute without '=', got nil")
	}
}

func TestRepositoryURL(t *testing.T) {
	tests := []struct {
		name       string
		attributes map[string]string
		url        string
		ok         bool
	}{
		{
			name:       "github with .git suffix",
			attributes: map[string]string{"protocol": "https", "host": "github.com", "path": "namespacelabs/internal.git"},
			url:        "https://github.com/namespacelabs/internal.git",
			ok:         true,
		},
		{
			name:       "cursor origin without .git suffix",
			attributes: map[string]string{"protocol": "https", "host": "origin.cursor.com", "path": "namespacelabs/internal"},
			url:        "https://origin.cursor.com/namespacelabs/internal",
			ok:         true,
		},
		{name: "ssh is not handled", attributes: map[string]string{"protocol": "ssh", "host": "github.com", "path": "org/repo"}, ok: false},
		{name: "missing path", attributes: map[string]string{"protocol": "https", "host": "github.com"}, ok: false},
		{name: "arbitrary host", attributes: map[string]string{"protocol": "https", "host": "gitlab.com", "path": "org/repo"}, url: "https://gitlab.com/org/repo", ok: true},
		{name: "missing host", attributes: map[string]string{"protocol": "https", "path": "org/repo"}, ok: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			url, ok := RepositoryURL(test.attributes)
			if ok != test.ok {
				t.Fatalf("RepositoryURL(%v) ok = %t, want %t", test.attributes, ok, test.ok)
			}
			if url != test.url {
				t.Errorf("RepositoryURL(%v) = %q, want %q", test.attributes, url, test.url)
			}
		})
	}
}

func basicAuthorization(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

func TestCredentialFromResponse(t *testing.T) {
	tests := []struct {
		name        string
		repoURL     string
		resp        *ObtainGitCredentialsForRepositoryResponse
		credential  *Credential
		expectError string
	}{
		{
			name:    "github basic credentials",
			repoURL: "https://github.com/namespacelabs/internal.git",
			resp: &ObtainGitCredentialsForRepositoryResponse{
				Provider: "PROVIDER_GITHUB",
				URLScope: "https://github.com/",
				Headers:  []Header{{Name: "Authorization", Value: basicAuthorization("x-access-token", "ghs_token")}},
			},
			credential: &Credential{Username: "x-access-token", Password: "ghs_token"},
		},
		{
			name:    "cursor origin basic credentials",
			repoURL: "https://origin.cursor.com/namespacelabs/internal.git",
			resp: &ObtainGitCredentialsForRepositoryResponse{
				Provider: "PROVIDER_CURSOR_ORIGIN",
				URLScope: "https://origin.cursor.com/",
				Headers:  []Header{{Name: "Authorization", Value: basicAuthorization("x-access-token", "cursor_token")}},
			},
			credential: &Credential{Username: "x-access-token", Password: "cursor_token"},
		},
		{
			name:       "no headers means no credentials",
			repoURL:    "https://github.com/namespacelabs/internal.git",
			resp:       &ObtainGitCredentialsForRepositoryResponse{Provider: "PROVIDER_GITHUB", URLScope: "https://github.com/"},
			credential: nil,
		},
		{
			name:       "password containing a colon survives",
			repoURL:    "https://github.com/o/r.git",
			resp:       &ObtainGitCredentialsForRepositoryResponse{URLScope: "https://github.com/", Headers: []Header{{Name: "Authorization", Value: basicAuthorization("user", "pass:with:colons")}}},
			credential: &Credential{Username: "user", Password: "pass:with:colons"},
		},
		{
			name:    "uppercase host request matches lowercase scope",
			repoURL: "https://github.com/o/r.git",
			resp: &ObtainGitCredentialsForRepositoryResponse{
				URLScope: "https://github.com/",
				Headers:  []Header{{Name: "Authorization", Value: basicAuthorization("x-access-token", "tok")}},
			},
			credential: &Credential{Username: "x-access-token", Password: "tok"},
		},
		{
			name:        "missing scope with headers is rejected",
			repoURL:     "https://github.com/o/r.git",
			resp:        &ObtainGitCredentialsForRepositoryResponse{Headers: []Header{{Name: "Authorization", Value: basicAuthorization("u", "p")}}},
			expectError: "has no URL scope",
		},
		{
			name:        "scope with different origin is rejected",
			repoURL:     "https://github.com/o/r.git",
			resp:        &ObtainGitCredentialsForRepositoryResponse{URLScope: "https://evil.example.com/", Headers: []Header{{Name: "Authorization", Value: basicAuthorization("u", "p")}}},
			expectError: "does not cover repository",
		},
		{
			name:        "scope with different port is rejected",
			repoURL:     "https://github.com/o/r.git",
			resp:        &ObtainGitCredentialsForRepositoryResponse{URLScope: "https://github.com:8443/", Headers: []Header{{Name: "Authorization", Value: basicAuthorization("u", "p")}}},
			expectError: "does not cover repository",
		},
		{
			name:        "basic payload without colon is rejected",
			repoURL:     "https://github.com/o/r.git",
			resp:        &ObtainGitCredentialsForRepositoryResponse{URLScope: "https://github.com/", Headers: []Header{{Name: "Authorization", Value: basicAuthorization("onlyusername", "")}}},
			expectError: "invalid basic authorization credentials",
		},
		{
			name:        "basic payload with empty password is rejected",
			repoURL:     "https://github.com/o/r.git",
			resp:        &ObtainGitCredentialsForRepositoryResponse{URLScope: "https://github.com/", Headers: []Header{{Name: "Authorization", Value: basicAuthorization("user", "")}}},
			expectError: "invalid basic authorization credentials",
		},
		{
			name:        "line-breaking credential bytes are rejected",
			repoURL:     "https://github.com/o/r.git",
			resp:        &ObtainGitCredentialsForRepositoryResponse{URLScope: "https://github.com/", Headers: []Header{{Name: "Authorization", Value: basicAuthorization("user", "pass\ninjected=true")}}},
			expectError: "invalid characters in credentials",
		},
		{
			name:        "headers without Authorization are an error",
			repoURL:     "https://github.com/o/r.git",
			resp:        &ObtainGitCredentialsForRepositoryResponse{URLScope: "https://github.com/", Headers: []Header{{Name: "X-Other", Value: "v"}}},
			expectError: "has no Authorization header",
		},
		{
			name:        "scope mismatch is rejected",
			repoURL:     "https://github.com/o/r.git",
			resp:        &ObtainGitCredentialsForRepositoryResponse{URLScope: "https://origin.cursor.com/", Headers: []Header{{Name: "Authorization", Value: basicAuthorization("u", "p")}}},
			expectError: "does not cover repository",
		},
		{
			name:        "scope path prefix is segment aware",
			repoURL:     "https://github.com/org/repository.git",
			resp:        &ObtainGitCredentialsForRepositoryResponse{URLScope: "https://github.com/org/repo", Headers: []Header{{Name: "Authorization", Value: basicAuthorization("u", "p")}}},
			expectError: "does not cover repository",
		},
		{
			name:        "non-basic scheme is rejected",
			repoURL:     "https://github.com/o/r.git",
			resp:        &ObtainGitCredentialsForRepositoryResponse{URLScope: "https://github.com/", Headers: []Header{{Name: "Authorization", Value: "Bearer tok"}}},
			expectError: "unsupported authorization scheme",
		},
		{
			name:        "malformed base64 is rejected",
			repoURL:     "https://github.com/o/r.git",
			resp:        &ObtainGitCredentialsForRepositoryResponse{URLScope: "https://github.com/", Headers: []Header{{Name: "Authorization", Value: "Basic !!!"}}},
			expectError: "invalid basic authorization header",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			credential, err := CredentialFromResponse(test.repoURL, test.resp)
			if test.expectError != "" {
				if err == nil || !strings.Contains(err.Error(), test.expectError) {
					t.Fatalf("CredentialFromResponse() error = %v, want it to contain %q", err, test.expectError)
				}
				return
			}
			if err != nil {
				t.Fatalf("CredentialFromResponse() error = %v", err)
			}
			if test.credential == nil {
				if credential != nil {
					t.Fatalf("CredentialFromResponse() = %v, want nil", credential)
				}
				return
			}
			if credential == nil || *credential != *test.credential {
				t.Fatalf("CredentialFromResponse() = %+v, want %+v", credential, test.credential)
			}
		})
	}
}

type testTokenSource struct {
	token string
}

func (ts testTokenSource) IssueToken(ctx context.Context, minDuration time.Duration, force bool) (string, error) {
	return ts.token, nil
}

func TestFetchAndGet(t *testing.T) {
	const repoURL = "https://github.com/namespacelabs/internal.git"

	var gotPath, gotAuthorization, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuthorization = r.Header.Get("Authorization")

		buf := &bytes.Buffer{}
		if _, err := buf.ReadFrom(r.Body); err != nil {
			t.Errorf("reading request body: %v", err)
		}
		gotBody = buf.String()

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"provider":"PROVIDER_GITHUB","urlScope":"https://github.com/","headers":[{"name":"Authorization","value":%q}],"expiresAt":"2026-09-09T12:00:00Z"}`,
			basicAuthorization("x-access-token", "ghs_token"))
	}))
	defer server.Close()

	ctx, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()

	resp, err := Fetch(ctx, server.URL+ObtainGitCredentialsPath, testTokenSource{token: "nsct_test"}, repoURL, server.Client())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if resp.Provider != "PROVIDER_GITHUB" || resp.URLScope != "https://github.com/" || len(resp.Headers) != 1 {
		t.Fatalf("unexpected response: %+v", resp)
	}

	if gotPath != "/nsl.secrets.SecretsService/ObtainGitCredentialsForRepository" {
		t.Errorf("request path = %q", gotPath)
	}
	if gotAuthorization != "Bearer nsct_test" {
		t.Errorf("request authorization = %q", gotAuthorization)
	}
	if gotBody != `{"repository_url":"`+repoURL+`"}` {
		t.Errorf("request body = %q", gotBody)
	}

	credential, err := CredentialFromResponse(repoURL, resp)
	if err != nil {
		t.Fatalf("CredentialFromResponse: %v", err)
	}
	if credential == nil || credential.Username != "x-access-token" || credential.Password != "ghs_token" {
		t.Fatalf("unexpected credential: %+v", credential)
	}
}

func TestGetDoesNotLeakCredentialsToDebugOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"provider":"PROVIDER_GITHUB","urlScope":"https://github.com/","headers":[{"name":"Authorization","value":%q}]}`,
			basicAuthorization("x-access-token", "super_secret_token"))
	}))
	defer server.Close()

	debug := &bytes.Buffer{}
	stdin := strings.NewReader("protocol=https\nhost=github.com\npath=org/repo.git\nusername=other\npassword=other_secret\noauth_refresh_token=refresh\n\n")
	err := Get(context.Background(), Options{
		Endpoint:    server.URL + ObtainGitCredentialsPath,
		TokenSource: testTokenSource{token: "nsct_test"},
		HTTPClient:  server.Client(),
		Debug:       debug,
	}, stdin, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	for _, secret := range []string{"super_secret_token", "x-access-token", "other_secret", "refresh"} {
		if strings.Contains(debug.String(), secret) {
			t.Errorf("debug output leaked %q: %s", secret, debug.String())
		}
	}
}

func TestGetIgnoresInvalidRequests(t *testing.T) {
	opts := Options{Endpoint: "http://localhost/unused", TokenSource: testTokenSource{token: "nsct_test"}}

	tests := []struct {
		name  string
		input string
	}{
		{name: "no path", input: "protocol=https\nhost=github.com\n"},
		{name: "ssh request", input: "protocol=ssh\nhost=github.com\npath=org/repo.git\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			err := Get(context.Background(), opts, strings.NewReader(test.input), out)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if out.Len() != 0 {
				t.Errorf("Get wrote %q, want no output", out.String())
			}
		})
	}
}

func TestGetErrorsOnServerFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("grpc-message", "permission denied")
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer server.Close()

	opts := Options{Endpoint: server.URL + ObtainGitCredentialsPath, TokenSource: testTokenSource{token: "nsct_test"}, HTTPClient: server.Client()}

	out := &bytes.Buffer{}
	err := Get(context.Background(), opts, strings.NewReader("protocol=https\nhost=github.com\npath=org/private.git\n"), out)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("Get() error = %v, want it to contain %q", err, "permission denied")
	}
}

func TestGetEndToEnd(t *testing.T) {
	var tokenSource api.TokenSource = testTokenSource{token: "nsct_test"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"provider":"PROVIDER_CURSOR_ORIGIN","urlScope":"https://origin.cursor.com/","headers":[{"name":"Authorization","value":%q}]}`,
			basicAuthorization("x-access-token", "cursor_token"))
	}))
	defer server.Close()

	out := &bytes.Buffer{}
	err := Get(context.Background(), Options{
		Endpoint:    server.URL + ObtainGitCredentialsPath,
		TokenSource: tokenSource,
		HTTPClient:  server.Client(),
	}, strings.NewReader("protocol=https\nhost=origin.cursor.com\npath=namespacelabs/internal.git\n\n"), out)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := "username=x-access-token\npassword=cursor_token\n\n"
	if out.String() != want {
		t.Errorf("Get output = %q, want %q", out.String(), want)
	}
}
