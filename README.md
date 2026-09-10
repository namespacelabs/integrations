# Namespace Integrations

## SDK

Under `api` you can find our SDK to access [Namespace](https://namespace.so) Compute/Storage APIs.
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
For large artifacts, `storage/downloader` supports parallel downloads, retries, and resuming partial downloads.
The public API definition can be found at [buf.build/namespace](https://buf.build/namespace/cloud/docs/main:namespace.cloud.storage.v1beta).

### Protobuf definitions

Generated Go protobuf and gRPC packages are checked in under `proto`.
Regenerate them from a sibling checkout of `namespacelabs/internal` with:

```sh
./scripts/generate-protos.sh
```

Set `NS_INTERNAL` to use an internal checkout in another location.

## Tools

This repository hosts a series of integration tools that can be used either
standalone, or with [Namespace](https://namespace.so)'s cloud.

- `fetch-gcp-secret`: A self-contained binary that fetches a secret managed by
  GCP Secret Manager into a local file. It also supports Namespace's GCP
  workload federation.
- `git-credential-nsc`: A generic git credential helper that resolves
  short-lived credentials for the repository git is cloning from
  (`ObtainGitCredentialsForRepository`), supporting every host the
  SecretsService does (GitHub, Cursor Origin). Unlike
  `git-credential-nsc-github-credentials`, it needs no `--repository` or
  `--secret_id` flags; install it with:
  ```sh
  git config --global credential.helper "/path/git-credential-nsc"
  git config --global credential.useHttpPath true
  ```
  (git only passes the repository path to helpers when `useHttpPath` is
  enabled; helper flags such as `--debug` go before the action, e.g.
  `git config --global credential.helper "/path/git-credential-nsc --debug"`).
- `git-credential-nsc-github-credentials`: A git credential helper that issues
  GitHub-only short-term tokens for a fixed `--repository`/`--secret_id` pair
  via `ObtainGitHubCredentials`.
