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
	"namespacelabs.dev/integrations/api"
	"namespacelabs.dev/integrations/nsc/grpcapi"
)

type Client struct {
	Artifacts storagev1betagrpc.ArtifactsServiceClient

	Conn *grpc.ClientConn
}

func NewClient(ctx context.Context, token api.TokenSource, opts ...grpc.DialOption) (Client, error) {
	if endpoint := os.Getenv("NSC_STORAGE_ENDPOINT"); endpoint != "" {
		return NewClientWithEndpoint(ctx, endpoint, token, opts...)
	}

	return NewClientWithEndpoint(ctx, "https://eu.storage.namespaceapis.com", token, opts...)
}

func NewClientWithEndpoint(ctx context.Context, endpoint string, token api.TokenSource, opts ...grpc.DialOption) (Client, error) {
	conn, err := grpcapi.NewConnectionWithEndpoint(ctx, endpoint, token, opts...)
	if err != nil {
		return Client{}, err
	}

	return Client{
		Artifacts: storagev1betagrpc.NewArtifactsServiceClient(conn),
		Conn:      conn,
	}, nil
}

func (c Client) Close() error {
	return c.Conn.Close()
}

func UploadArtifact(ctx context.Context, c Client, namespace, path string, in io.Reader) error {
	res, err := c.Artifacts.CreateArtifact(ctx, &storagev1beta.CreateArtifactRequest{
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
		if _, err := io.ReadAll(httpRes.Body); err != nil {
			return fmt.Errorf("reading response body: %w", err)
		}

		return fmt.Errorf("failed to upload file: status %d", httpRes.StatusCode)
	}

	if _, err := c.Artifacts.FinalizeArtifact(ctx, &storagev1beta.FinalizeArtifactRequest{
		Path:      path,
		Namespace: namespace,
		UploadId:  res.UploadId,
	}); err != nil {
		return err
	}

	return nil
}

func ResolveArtifactStream(ctx context.Context, c Client, namespace, path string) (io.ReadCloser, error) {
	res, err := c.Artifacts.ResolveArtifact(ctx, &storagev1beta.ResolveArtifactRequest{
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
		return nil, fmt.Errorf("failed to download file: status %d", httpRes.StatusCode)
	}

	return httpRes.Body, nil
}
