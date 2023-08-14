# Overview

`fetch-gcp-secret` is a self-contained binary that fetches a secret managed by
GCP Secret Manager into a local file. It also supports Namespace's GCP
workload federation.

It can be used e.g. in CI to configure a particular pipeline run with secrets
that are persisted in GCP.

## Building

Go 1.20+ is required to build `fetch-gcp-secret`.

```sh
cd integrations
go mod download
go build ./cmd/fetch-gcp-secret
```

## Using within GCP

Ensure that the Compute Engine instance is started with a service account that has `Secret Manager Secret Accessor` permissions on the secret to be read. This can be accomplished by either changing the permissions of the default compute service account, or by creating a separate service account with the appropriate permissions.

```sh
fetch-gcp-secret \
  --to /root/.ssh/id_rsa \
  --secret_name projects/73359809572/secrets/SSH_KEY/versions/latest
```

## Using within Namespace

First set up workload identity federation by following https://cloud.namespace.so/docs/federation/gcp

```sh
fetch-gcp-secret \
  --to /root/.ssh/id_rsa \
  --secret_name projects/73359809572/secrets/SSH_KEY/versions/latest \
  --workload_identity_provider https://iam.googleapis.com/projects/73359809572/locations/global/workloadIdentityPools/... \
  --service_account SERVICE_ACCOUNT
```