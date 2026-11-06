// Copyright 2022 Namespace Labs Inc; All rights reserved.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package downloader

import (
	"context"
	"fmt"

	storageapi "namespacelabs.dev/integrations/api/storage"
	storagev1beta "namespacelabs.dev/integrations/proto/namespace/cloud/storage/v1beta"
)

// DownloadArtifact downloads an artifact to destPath. Signed download URLs are
// refreshed for each request, but must refer to the initially resolved version.
func DownloadArtifact(ctx context.Context, cli storageapi.Client, namespace, path, destPath string, opts Options) error {
	req := &storagev1beta.ResolveArtifactRequest{Namespace: namespace, Path: path}
	initial, err := cli.Artifacts.ResolveArtifact(ctx, req)
	if err != nil {
		return err
	}
	opts.sourceID = initial.GetDescription().GetId()
	opts.ResolveURL = func(ctx context.Context) (string, error) {
		res, err := cli.Artifacts.ResolveArtifact(ctx, req)
		if err != nil {
			return "", err
		}
		if res.GetDescription().GetId() != opts.sourceID {
			return "", fmt.Errorf("artifact changed during download: %s/%s; restart the download", namespace, path)
		}
		return res.GetSignedDownloadUrl(), nil
	}
	return Download(ctx, destPath, opts)
}
