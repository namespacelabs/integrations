package auth

import (
	"errors"
	"strings"

	"github.com/golang-jwt/jwt/v4"
)

var ErrNotLoggedIn = errors.New("not logged in")

type TokenClaims struct {
	jwt.RegisteredClaims

	TenantID       string `json:"tenant_id"`
	ActorID        string `json:"actor_id"`
	InstanceID     string `json:"instance_id"`
	OwnerID        string `json:"owner_id"`
	PrimaryRegion  string `json:"primary_region"`
	WorkloadRegion string `json:"workload_region"`
}

func ExtractClaims(token string) (*TokenClaims, error) {
	switch {
	case strings.HasPrefix(token, "st_"):
		return parseClaims(strings.TrimPrefix(token, "st_"))
	case strings.HasPrefix(token, "nsct_"):
		return parseClaims(strings.TrimPrefix(token, "nsct_"))
	case strings.HasPrefix(token, "nscw_"):
		return parseClaims(strings.TrimPrefix(token, "nscw_"))
	default:
		return nil, ErrNotLoggedIn
	}
}

func parseClaims(raw string) (*TokenClaims, error) {
	parser := jwt.Parser{}

	var claims TokenClaims
	if _, _, err := parser.ParseUnverified(raw, &claims); err != nil {
		return nil, ErrNotLoggedIn
	}

	return &claims, nil
}
