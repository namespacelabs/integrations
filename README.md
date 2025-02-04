# Namespace Integrations

## SDK

Under `api` you can find our SDK to access Namespace Compute/Storage APIs.
The SDK also handles credential management for you:

- From a workstation, run `nsc login`, and then run `auth.LoadUsertoken()`
- From an instance run `auth.LoadWorkloadToken()` (it uses a per-instance workload identity system)
- Or to handle either, just do `auth.LoadDefaults()`

And then use the resulting `api.TokenSource` with the APIs.

### Compute SDK

The Namespace Compute SDK can be found at `api/compute`.
It provides GRPC clients ready to use.
The public API definition can be found at [buf.build/namespace](https://buf.build/namespace/cloud/docs/main:namespace.cloud.compute.v1beta).

### Storage SDK

The Namespace Storage SDK can be found at `api/storage`.
It provides GRPC clients ready to use.
Also, it provides convenience wrappers to simplify the upload/download of artifacts using the `io.Reader` API.
The public API definition can be found at [buf.build/namespace](https://buf.build/namespace/cloud/docs/main:namespace.cloud.storage.v1beta).

## Tools

This repository hosts a series of integration tools that can be used either
standalone, or with [Namespace](https://namespace.so)'s cloud.

- `fetch-gcp-secret`: A self-contained binary that fetches a secret managed by
  GCP Secret Manager into a local file. It also supports Namespace's GCP
  workload federation.
