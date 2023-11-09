package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Token struct {
	BearerToken string `json:"bearer_token"`
}

func LoadUserToken() (Token, error) {
	dir, err := configDir()
	if err != nil {
		return Token{}, err
	}

	token, err := loadFromFile(filepath.Join(dir, "token.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return Token{}, fmt.Errorf("you are not logged in to Namespace; try running `nsc login`")
		}
	}

	return token, err
}

func LoadWorkloadToken() (Token, error) {
	if tf := os.Getenv("NSC_TOKEN_FILE"); tf != "" {
		return loadFromFile(tf)
	}

	return loadFromFile("/var/run/nsc/token.json")
}

func loadFromFile(tokenFile string) (Token, error) {
	contents, err := os.ReadFile(tokenFile)
	if err != nil {
		return Token{}, err
	}

	var tj Token
	if err := json.Unmarshal(contents, &tj); err != nil {
		return Token{}, fmt.Errorf("failed to parse token file: %w", err)
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
