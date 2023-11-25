package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"namespacelabs.dev/integrations/nsc"
)

type Token struct {
	BearerToken string `json:"bearer_token"`
}

func (t Token) IssueToken(_ context.Context) (string, error) {
	return t.BearerToken, nil
}

func LoadDefaults() (nsc.TokenSource, error) {
	if tf := os.Getenv("NSC_TOKEN_FILE"); tf != "" {
		return loadFromFile(tf)
	}

	t, err := loadFromFile("/var/run/nsc/token.json")
	if err == nil {
		return t, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}

	return LoadUserToken()
}

func LoadUserToken() (nsc.TokenSource, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}

	token, err := loadFromFile(filepath.Join(dir, "token.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("you are not logged in to Namespace; try running `nsc login`")
		}
	}

	return token, err
}

func LoadWorkloadToken() (nsc.TokenSource, error) {
	if tf := os.Getenv("NSC_TOKEN_FILE"); tf != "" {
		return loadFromFile(tf)
	}

	return loadFromFile("/var/run/nsc/token.json")
}

func loadFromFile(tokenFile string) (nsc.TokenSource, error) {
	contents, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, err
	}

	var tj Token
	if err := json.Unmarshal(contents, &tj); err != nil {
		return nil, fmt.Errorf("failed to parse token file: %w", err)
	}

	return tj, nil
}

func configDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return dir, err
	}

	return filepath.Join(dir, "ns"), nil
}
