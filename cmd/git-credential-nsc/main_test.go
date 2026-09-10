package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"namespacelabs.dev/integrations/pkg/gitcredentials"
)

type devnull struct{ t *testing.T }

func (d devnull) Read([]byte) (int, error) {
	d.t.Helper()
	d.t.Errorf("credential input was read for a non-get action")
	return 0, os.ErrClosed
}

func testEndpoint(t *testing.T, hit func(t *testing.T, r *http.Request)) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hit != nil {
			hit(t, r)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	return server.URL + gitcredentials.ObtainGitCredentialsPath
}

func TestNonGetActionsDoNothing(t *testing.T) {
	endpoint := testEndpoint(t, func(t *testing.T, r *http.Request) {
		t.Errorf("unexpected request to the secrets service: %s", r.URL.Path)
	})

	t.Setenv("NSC_IAM_ENDPOINT", strings.TrimSuffix(endpoint, gitcredentials.ObtainGitCredentialsPath))

	for _, action := range []string{"store", "erase"} {
		stdout := &strings.Builder{}
		if code := run([]string{action}, devnull{t}, stdout, &strings.Builder{}); code != 0 {
			t.Errorf("run(%q) = %d, want 0", action, code)
		}
		if stdout.Len() != 0 {
			t.Errorf("run(%q) wrote %q to stdout", action, stdout.String())
		}
	}
}

func TestNonGetActionsAreSilent(t *testing.T) {
	endpoint := testEndpoint(t, func(t *testing.T, r *http.Request) {
		t.Errorf("unexpected request to the secrets service: %s", r.URL.Path)
	})
	t.Setenv("NSC_IAM_ENDPOINT", strings.TrimSuffix(endpoint, gitcredentials.ObtainGitCredentialsPath))

	for _, action := range []string{"capability", "store", "erase", "bogus"} {
		stdout := &strings.Builder{}
		if code := run([]string{action}, devnull{t}, stdout, &strings.Builder{}); code != 0 {
			t.Errorf("run(%q) = %d, want 0", action, code)
		}
		if stdout.Len() != 0 {
			t.Errorf("run(%q) wrote %q to stdout", action, stdout.String())
		}
	}
}

func TestInvalidInvocations(t *testing.T) {
	endpoint := testEndpoint(t, func(t *testing.T, r *http.Request) {
		t.Errorf("unexpected request to the secrets service: %s", r.URL.Path)
	})
	t.Setenv("NSC_IAM_ENDPOINT", strings.TrimSuffix(endpoint, gitcredentials.ObtainGitCredentialsPath))

	if code := run(nil, devnull{t}, &strings.Builder{}, &strings.Builder{}); code != 0 {
		t.Errorf("run(no args) = %d, want 0", code)
	}
	if code := run([]string{"--validate"}, devnull{t}, &strings.Builder{}, &strings.Builder{}); code != 2 {
		t.Errorf("run(--validate without --repository) = %d, want 2", code)
	}
	if code := run([]string{"--validate", "--repository", "not a URL"}, devnull{t}, &strings.Builder{}, &strings.Builder{}); code != 1 {
		t.Errorf("run(--validate on invalid URL) = %d, want 1", code)
	}

	for _, repository := range []string{
		"https://user:super_secret@git.example.com/org/repo.git",
		"https://git.example.com/org/repo.git?token=super_secret",
		"https://git.example.com/org/repo.git#super_secret",
	} {
		stdout := &strings.Builder{}
		stderr := &strings.Builder{}
		if code := run([]string{"--validate", "--repository", repository}, devnull{t}, stdout, stderr); code != 1 {
			t.Errorf("run(--validate --repository %q) = %d, want 1", repository, code)
		}
		if strings.Contains(stdout.String(), "super_secret") || strings.Contains(stderr.String(), "super_secret") {
			t.Errorf("validation output leaked repository credentials: stdout=%q stderr=%q", stdout, stderr)
		}
	}
}

func TestGetEndToEndViaRun(t *testing.T) {
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:issued_token_123"))

	var contacted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contacted = true
		if got := r.Header.Get("Authorization"); got != "Bearer test_bearer" {
			t.Errorf("request authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"provider":"PROVIDER_GITHUB","urlScope":"https://github.com/","headers":[{"name":"Authorization","value":"` + basic + `"}]}`))
	}))
	defer server.Close()

	tokenFile := filepath.Join(t.TempDir(), "token.json")
	if err := os.WriteFile(tokenFile, []byte(`{"bearer_token":"test_bearer"}`), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("NSC_IAM_ENDPOINT", server.URL)
	t.Setenv("NSC_TOKEN_FILE", tokenFile)

	stdin := strings.NewReader("protocol=https\nhost=github.com\npath=namespacelabs/internal.git\n\n")
	stdout := &strings.Builder{}
	debug := &strings.Builder{}
	if code := run([]string{"--debug", "get"}, stdin, stdout, debug); code != 0 {
		t.Fatalf("run(get) = %d, debug output: %s", code, debug.String())
	}
	if !contacted {
		t.Errorf("the secrets service was not contacted")
	}
	if want := "username=x-access-token\npassword=issued_token_123\n\n"; stdout.String() != want {
		t.Errorf("run(get) stdout = %q, want %q", stdout.String(), want)
	}

	for _, secret := range []string{"issued_token_123", "x-access-token"} {
		if strings.Contains(debug.String(), secret) {
			t.Errorf("debug output leaked %q: %s", secret, debug.String())
		}
	}
	if !strings.Contains(debug.String(), "https://github.com/namespacelabs/internal.git") {
		t.Errorf("debug output should log the validated repository URL: %s", debug.String())
	}
}
