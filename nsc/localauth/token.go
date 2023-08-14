package localauth

import (
	"encoding/json"
	"os"
)

type TokenJson struct {
	BearerToken string `json:"bearer_token"`
}

func LoadToken() (TokenJson, error) {
	tokenFile := "/var/run/nsc/token.json"
	if tf := os.Getenv("NSC_TOKEN_FILE"); tf != "" {
		tokenFile = tf
	}

	contents, err := os.ReadFile(tokenFile)
	if err != nil {
		return TokenJson{}, err
	}

	var tj TokenJson
	if err := json.Unmarshal(contents, &tj); err != nil {
		return TokenJson{}, err
	}

	return tj, nil
}
