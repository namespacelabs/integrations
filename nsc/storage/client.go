package storage

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"

	"buf.build/gen/go/namespace/cloud/grpc/go/proto/namespace/cloud/storage/v1beta/storagev1betagrpc"
	storagev1beta "buf.build/gen/go/namespace/cloud/protocolbuffers/go/proto/namespace/cloud/storage/v1beta"
	"google.golang.org/grpc"
	"namespacelabs.dev/integrations/nsc"
	"namespacelabs.dev/integrations/nsc/grpcapi"
)

type Client struct {
	Artifact storagev1betagrpc.ArtifactServiceClient

	Conn *grpc.ClientConn
}

func NewClient(ctx context.Context, token nsc.TokenSource, opts ...grpc.DialOption) (Client, error) {
	if endpoint := os.Getenv("NSC_ENDPOINT"); endpoint != "" {
		return NewClientWithEndpoint(ctx, endpoint, token)
	}

	return NewClientWithEndpoint(ctx, "https://eu.storage.namespaceapis.com", token, opts...)
}

func NewClientWithEndpoint(ctx context.Context, endpoint string, token nsc.TokenSource, opts ...grpc.DialOption) (Client, error) {
	conn, err := grpcapi.NewConnectionWithEndpoint(ctx, endpoint, token, opts...)
	if err != nil {
		return Client{}, err
	}

	return Client{
		Artifact: storagev1betagrpc.NewArtifactServiceClient(conn),
		Conn:     conn,
	}, nil
}

func (c Client) Close() error {
	return c.Conn.Close()
}

func (c Client) UploadArtifact(ctx context.Context, path, namespace string, in io.Reader) error {
	res, err := c.Artifact.CreateArtifact(ctx, &storagev1beta.CreateArtifactRequest{
		Path:      path,
		Namespace: namespace,
	})
	if err != nil {
		return err
	}

	var tmp bytes.Buffer
	n, err := io.Copy(&tmp, in)
	if err != nil {
		return fmt.Errorf("failed to copy input: %w", err)

	}

	h := md5.New()
	h.Write(tmp.Bytes())
	md5s := base64.StdEncoding.EncodeToString(h.Sum(nil))

	httpReq, err := http.NewRequestWithContext(ctx, "PUT", res.SignedUploadUrl, &tmp)
	if err != nil {
		return fmt.Errorf("failed to construct http request: %w", err)

	}

	httpReq.Header.Set("Content-Length", fmt.Sprintf("%d", n))
	httpReq.Header.Set("Content-MD5", md5s)

	httpRes, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to upload file: %w", err)
	}

	if httpRes.StatusCode != http.StatusOK {
		respBody, err := io.ReadAll(httpRes.Body)
		if err != nil {
			return fmt.Errorf("reading response body: %w", err)
		}

		fmt.Fprintf(os.Stderr, "Response Body: %s\n", respBody)

		return fmt.Errorf("failed to upload file: status %d", httpRes.StatusCode)
	}

	if _, err := c.Artifact.FinalizeArtifact(ctx, &storagev1beta.FinalizeArtifactRequest{
		Path:      path,
		Namespace: namespace,
		UploadId:  res.UploadId,
	}); err != nil {
		return err
	}

	return nil
}

func (c Client) DownloadArtifact(ctx context.Context, path, namespace string) (io.Reader, error) {
	res, err := c.Artifact.ResolveArtifact(ctx, &storagev1beta.ResolveArtifactRequest{
		Path:      path,
		Namespace: namespace,
	})
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "GET", res.SignedDownloadUrl, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to construct http request: %w", err)

	}
	httpRes, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to download file: %w", err)
	}

	if httpRes.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to upload file: status %d", httpRes.StatusCode)
	}

	return httpRes.Body, nil
}
