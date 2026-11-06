// Copyright 2022 Namespace Labs Inc; All rights reserved.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package downloader

import (
	"context"

	storageapi "namespacelabs.dev/integrations/api/storage"
	storagev1beta "namespacelabs.dev/integrations/proto/namespace/cloud/storage/v1beta"
)

// DownloadArtifact downloads one version of an artifact to destPath.
func DownloadArtifact(ctx context.Context, cli storageapi.Client, namespace, path, destPath string, opts Options) error {
	res, err := cli.Artifacts.ResolveArtifact(ctx, &storagev1beta.ResolveArtifactRequest{
		Namespace: namespace,
		Path:      path,
	})
	if err != nil {
		return err
	}
	// The path may be republished during the download; the signed URL pins its blob.
	opts.ResolveURL = func(context.Context) (string, error) {
		return res.GetSignedDownloadUrl(), nil
	}
	opts.sourceID = res.GetDescription().GetId()
	return Download(ctx, destPath, opts)
}
