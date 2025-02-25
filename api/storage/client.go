package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"

	"buf.build/gen/go/namespace/cloud/grpc/go/proto/namespace/cloud/storage/v1beta/storagev1betagrpc"
	storagev1beta "buf.build/gen/go/namespace/cloud/protocolbuffers/go/proto/namespace/cloud/storage/v1beta"
	"buf.build/gen/go/namespace/cloud/protocolbuffers/go/proto/namespace/stdlib"
	"google.golang.org/grpc"
	"namespacelabs.dev/integrations/api"
	"namespacelabs.dev/integrations/nsc/grpcapi"
)

const (
	ArtifactDigestLabel = "artifact.digest.sha256"
)

type Client struct {
	Artifacts storagev1betagrpc.ArtifactsServiceClient

	Conn *grpc.ClientConn
}

func NewClient(ctx context.Context, token api.TokenSource, opts ...grpc.DialOption) (Client, error) {
	if endpoint := os.Getenv("NSC_STORAGE_ENDPOINT"); endpoint != "" {
		return NewClientWithEndpoint(ctx, endpoint, token, opts...)
	}

	return NewClientWithEndpoint(ctx, "https://ord.storage.namespaceapis.com", token, opts...)
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
	return UploadArtifactWithOpts(ctx, c, namespace, path, in, UploadOpts{})
}

type UploadOpts struct {
	// Artifact labels to save.
	Labels map[string]string

	// Expected length of the uploaded content.
	// If not set and the Reader is a Seeker, the length will be determined automatically.
	// Otherwise the content will be buffered in memory.
	Length int64

	// Expected MD5 of the uploaded content; optional.
	MD5 string
}

func UploadArtifactWithOpts(ctx context.Context, c Client, namespace, path string, in io.Reader, o UploadOpts) error {
	var labelRecords []*stdlib.Label
	for k, v := range o.Labels {
		labelRecords = append(labelRecords, &stdlib.Label{Name: k, Value: v})
	}

	res, err := c.Artifacts.CreateArtifact(ctx, &storagev1beta.CreateArtifactRequest{
		Path:      path,
		Namespace: namespace,
		Labels:    labelRecords,
	})
	if err != nil {
		return err
	}

	length := o.Length

	// Zero length could be a valid explicitly set value, but we it's also the default value.
	// We don't want to force the user to explicitly set it to -1, so we may do useless work of counting 0 bytes again.
	if length <= 0 {
		if s, ok := in.(io.Seeker); ok {
			n, err := getReaderLength(s)
			if err != nil {
				return fmt.Errorf("failed to determine input length by seeking: %w", err)
			}
			length = n
		}
	}

	if length <= 0 {
		tmp := bytes.NewBuffer(nil)
		n, err := io.Copy(tmp, in)
		if err != nil {
			return fmt.Errorf("failed to buffer input: %w", err)
		}

		in = tmp
		length = n
	}

	hasher := sha256.New()
	body := io.TeeReader(in, hasher)

	httpReq, err := http.NewRequestWithContext(ctx, "PUT", res.SignedUploadUrl, body)
	if err != nil {
		return fmt.Errorf("failed to construct http request: %w", err)
	}

	// This also disables chunked encoding, which is unsupported by MinIO.
	httpReq.ContentLength = length

	if o.MD5 != "" {
		httpReq.Header.Set("Content-MD5", o.MD5)
	}

	httpRes, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to upload file: %w", err)
	}
	httpRes.Body.Close()

	if httpRes.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to upload file: status %d", httpRes.StatusCode)
	}

	if _, err := c.Artifacts.FinalizeArtifact(ctx, &storagev1beta.FinalizeArtifactRequest{
		Path:      path,
		Namespace: namespace,
		UploadId:  res.UploadId,
		AddLabels: []*stdlib.Label{{Name: ArtifactDigestLabel, Value: hex.EncodeToString(hasher.Sum(nil))}},
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

func getReaderLength(r io.Seeker) (int64, error) {
	cur, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}

	end, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}

	_, err = r.Seek(cur, io.SeekStart)
	if err != nil {
		return 0, err
	}

	return end - cur, nil
}
