// Copyright 2022 Namespace Labs Inc; All rights reserved.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package downloader

import (
	"context"

	storageapi "namespacelabs.dev/integrations/api/storage"
	storagev1beta "namespacelabs.dev/integrations/proto/namespace/cloud/storage/v1beta"
)

// DownloadArtifact downloads an artifact to destPath. Signed download URLs are
// resolved again for each request so retries do not depend on an expired URL.
func DownloadArtifact(ctx context.Context, cli storageapi.Client, namespace, path, destPath string, opts Options) error {
	opts.ResolveURL = func(ctx context.Context) (string, error) {
		res, err := cli.Artifacts.ResolveArtifact(ctx, &storagev1beta.ResolveArtifactRequest{
			Namespace: namespace,
			Path:      path,
		})
		if err != nil {
			return "", err
		}
		return res.GetSignedDownloadUrl(), nil
	}
	return Download(ctx, destPath, opts)
}
