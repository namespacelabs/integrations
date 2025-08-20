package builds

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"namespacelabs.dev/integrations/api"
	"namespacelabs.dev/integrations/auth"
)

func NSCRBase(ctx context.Context, token api.TokenSource) (string, error) {
	if reg := os.Getenv("NSC_REGISTRY"); reg != "" {
		return reg, nil
	}

	t, err := token.IssueToken(ctx, time.Minute, false)
	if err != nil {
		return "", err
	}

	claims, err := auth.ExtractClaims(t)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("nscr.io/%s", strings.TrimPrefix(claims.TenantID, "tenant_")), nil
}

func NSCRImage(ctx context.Context, token api.TokenSource, name string) (string, error) {
	reg, err := NSCRBase(ctx, token)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s/%s", reg, name), nil
}
