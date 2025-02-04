package auth

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/golang-jwt/jwt/v4"
)

var ErrNotLoggedIn = errors.New("not logged in")

type TokenClaims struct {
	jwt.RegisteredClaims

	TenantID      string `json:"tenant_id"`
	InstanceID    string `json:"instance_id"`
	OwnerID       string `json:"owner_id"`
	PrimaryRegion string `json:"primary_region"`
}

func tokenClaims(debugLog io.Writer, token string) (*TokenClaims, error) {
	switch {
	case strings.HasPrefix(token, "st_"):
		return parseClaims(debugLog, strings.TrimPrefix(token, "st_"))
	case strings.HasPrefix(token, "nsct_"):
		return parseClaims(debugLog, strings.TrimPrefix(token, "nsct_"))
	case strings.HasPrefix(token, "nscw_"):
		return parseClaims(debugLog, strings.TrimPrefix(token, "nscw_"))
	default:
		return nil, ErrNotLoggedIn
	}
}

func parseClaims(debugLog io.Writer, raw string) (*TokenClaims, error) {
	parser := jwt.Parser{}

	var claims TokenClaims
	if _, _, err := parser.ParseUnverified(raw, &claims); err != nil {
		fmt.Fprintf(debugLog, "parsing claims %q failed: %v\n", raw, err)
		return nil, ErrNotLoggedIn
	}

	return &claims, nil
}
