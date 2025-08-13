package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"buf.build/gen/go/namespace/cloud/grpc/go/proto/namespace/cloud/storage/v1beta/storagev1betagrpc"
	storagev1beta "buf.build/gen/go/namespace/cloud/protocolbuffers/go/proto/namespace/cloud/storage/v1beta"
	"buf.build/gen/go/namespace/cloud/protocolbuffers/go/proto/namespace/stdlib"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"namespacelabs.dev/integrations/api"
	"namespacelabs.dev/integrations/nsc/grpcapi"
)

const (
	artifactDigestLabel    = "artifact.digest.sha256"
	cacheSourceURLLabel    = "cache.source-url"
	cacheArtifactNamespace = "cache"
)

var (
	// Matches server-side validation regexp.
	validPath = regexp.MustCompile("[a-zA-Z0-9][a-zA-Z0-9-_./]*[a-zA-Z0-9]")
	slashRuns = regexp.MustCompile("/+")
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
	_, err := UploadArtifactWithOpts(ctx, c, namespace, path, in, UploadOpts{})
	return err
}

type UploadOpts struct {
	// Artifact labels to save.
	Labels map[string]string

	// Sets an expiry. Optional.
	ExpiresAt *time.Time

	// Expected length of the uploaded content.
	// If not set and the Reader is a Seeker, the length will be determined automatically.
	// Otherwise the content will be buffered in memory.
	Length int64

	// Expected MD5 of the uploaded content; optional.
	MD5 string
}

type ArtifactInfo struct {
	// Timestamp of the artifact commit.
	CreatedAt time.Time

	// Digest of the uploaded content.
	DigestSHA256 string
}

func UploadArtifactWithOpts(ctx context.Context, c Client, namespace, path string, in io.Reader, o UploadOpts) (ArtifactInfo, error) {
	var labelRecords []*stdlib.Label
	for k, v := range o.Labels {
		labelRecords = append(labelRecords, &stdlib.Label{Name: k, Value: v})
	}

	req := &storagev1beta.CreateArtifactRequest{
		Path:      path,
		Namespace: namespace,
		Labels:    labelRecords,
	}

	if o.ExpiresAt != nil {
		req.ExpiresAt = timestamppb.New(*o.ExpiresAt)
	}

	res, err := c.Artifacts.CreateArtifact(ctx, req)
	if err != nil {
		return ArtifactInfo{}, err
	}

	length := o.Length

	// Zero length could be a valid explicitly set value, but we it's also the default value.
	// We don't want to force the user to explicitly set it to -1, so we may do useless work of counting 0 bytes again.
	if length <= 0 {
		if s, ok := in.(io.Seeker); ok {
			n, err := getReaderLength(s)
			if err != nil {
				return ArtifactInfo{}, fmt.Errorf("failed to determine input length by seeking: %w", err)
			}
			length = n
		}
	}

	if length <= 0 {
		tmp := bytes.NewBuffer(nil)
		n, err := io.Copy(tmp, in)
		if err != nil {
			return ArtifactInfo{}, fmt.Errorf("failed to buffer input: %w", err)
		}

		in = tmp
		length = n
	}

	hasher := sha256.New()
	body := io.TeeReader(in, hasher)

	httpReq, err := http.NewRequestWithContext(ctx, "PUT", res.SignedUploadUrl, body)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to construct http request: %w", err)
	}

	// This also disables chunked encoding, which is unsupported by MinIO.
	httpReq.ContentLength = length

	if o.MD5 != "" {
		httpReq.Header.Set("Content-MD5", o.MD5)
	}

	httpRes, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to upload file: %w", err)
	}
	httpRes.Body.Close()

	if httpRes.StatusCode != http.StatusOK {
		return ArtifactInfo{}, fmt.Errorf("failed to upload file: status %d", httpRes.StatusCode)
	}

	digest := hex.EncodeToString(hasher.Sum(nil))

	finalizeResp, err := c.Artifacts.FinalizeArtifact(ctx, &storagev1beta.FinalizeArtifactRequest{
		Path:      path,
		Namespace: namespace,
		UploadId:  res.UploadId,
		AddLabels: []*stdlib.Label{{Name: artifactDigestLabel, Value: digest}},
	})
	if err != nil {
		return ArtifactInfo{}, err
	}

	return ArtifactInfo{
		CreatedAt:    finalizeResp.GetDescription().GetCreatedAt().AsTime(),
		DigestSHA256: digest,
	}, nil
}

func ResolveArtifactStream(ctx context.Context, cli Client, namespace, path string) (io.ReadCloser, error) {
	r, _, err := ResolveArtifactWithOpts(ctx, cli, namespace, path, ResolveArtifactOpts{})
	return r, err
}

type ResolveArtifactOpts struct{}

func ResolveArtifactWithOpts(ctx context.Context, cli Client, namespace, path string, opts ResolveArtifactOpts) (io.ReadCloser, ArtifactInfo, error) {
	res, err := cli.Artifacts.ResolveArtifact(ctx, &storagev1beta.ResolveArtifactRequest{
		Path:      path,
		Namespace: namespace,
	})
	if err != nil {
		return nil, ArtifactInfo{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "GET", res.SignedDownloadUrl, nil)
	if err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("failed to construct http request: %w", err)
	}

	httpRes, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("failed to download file: %w", err)
	}

	if httpRes.StatusCode != http.StatusOK {
		return nil, ArtifactInfo{}, fmt.Errorf("failed to download file: status %d", httpRes.StatusCode)
	}

	var digest string
	for _, lbl := range res.GetDescription().GetLabels() {
		if lbl.GetName() == artifactDigestLabel {
			digest = lbl.GetValue()
		}
	}

	return httpRes.Body, ArtifactInfo{
		CreatedAt:    res.GetDescription().GetCreatedAt().AsTime(),
		DigestSHA256: digest,
	}, nil
}

type CacheURLOpts struct {
	// Re-download from source if cached data is older than that.
	NewerThan time.Time

	// Verify digest of the cached or downloaded data.
	ExpectedSHA256 string

	// Receives human-readable debug messages.
	Logf func(string, ...interface{})
}

type CacheInfo struct {
	CachedAt time.Time
}

// Download the content from an arbitrary URL and cache it for fast access.
//
// The content at the URL is assumed to be immutable. If content is already present in the artifact cache for the given URL it will be used instead.
//
// Returns CacheSourceError if it fails downloading from sourceURL.
func CacheURL(ctx context.Context, cli Client, sourceURL string, opts CacheURLOpts) (io.ReadCloser, CacheInfo, error) {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...interface{}) {}
	}

	parsedURL, err := url.Parse(sourceURL)
	if err != nil {
		return nil, CacheInfo{}, status.Errorf(codes.InvalidArgument, "invalid URL format: %v", err)
	}

	labelFilter := []*stdlib.LabelFilterEntry{{Name: cacheSourceURLLabel, Value: sourceURL, Op: stdlib.LabelFilterEntry_EQUAL}}
	if opts.ExpectedSHA256 != "" {
		labelFilter = append(labelFilter, &stdlib.LabelFilterEntry{Name: artifactDigestLabel, Value: opts.ExpectedSHA256, Op: stdlib.LabelFilterEntry_EQUAL})
	}

	listResp, err := cli.Artifacts.ListArtifacts(ctx, &storagev1beta.ListArtifactsRequest{
		Namespaces:  []string{cacheArtifactNamespace},
		LabelFilter: labelFilter,
	})
	if err != nil {
		return nil, CacheInfo{}, fmt.Errorf("failed to list artifacts: %w", err)
	}

	var newest *storagev1beta.Artifact
	for _, art := range listResp.Artifacts {
		if newest == nil || art.GetCreatedAt().AsTime().After(newest.GetCreatedAt().AsTime()) {
			newest = art
		}
	}

	if newest != nil {
		if newest.GetCreatedAt().AsTime().After(opts.NewerThan) {
			logf("Loading from cache (cached at %v)...\n", newest.GetCreatedAt().AsTime())

			r, ai, err := ResolveArtifactWithOpts(ctx, cli, newest.GetNamespace(), newest.GetPath(), ResolveArtifactOpts{})
			if err != nil {
				return nil, CacheInfo{}, err
			}

			return r, CacheInfo{CachedAt: ai.CreatedAt}, nil
		} else {
			logf("Content cached at %v is too old, loading from source...\n", newest.GetCreatedAt().AsTime())
			// Fallthrough
		}
	} else {
		logf("Artifact not found in cache; loading from source...\n")
		// Fallthough
	}

	cachePath := cacheArtifactPath(time.Now(), parsedURL)
	labels := map[string]string{cacheSourceURLLabel: sourceURL}

	req, err := http.NewRequestWithContext(ctx, "GET", sourceURL, nil)
	if err != nil {
		return nil, CacheInfo{}, fmt.Errorf("failed to prepare request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, CacheInfo{}, CacheSourceError{fmt.Errorf("failed to send request: %w", err), 0}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, CacheInfo{}, CacheSourceError{fmt.Errorf("remote server returned status code %d", resp.StatusCode), resp.StatusCode}
	}

	w, err := os.CreateTemp("", "cache-")
	if err != nil {
		return nil, CacheInfo{}, fmt.Errorf("failed to open temp file: %w", err)
	}

	if err := os.Remove(w.Name()); err != nil {
		return nil, CacheInfo{}, fmt.Errorf("failed to unlink temp file: %w", err)
	}

	success := false
	defer func() {
		if !success {
			w.Close()
		}
	}()

	// Uploading the response will write it to the file as a side-effect.
	// Read errors will be tagged to allow the client to retry if needed.
	teeR := io.TeeReader(wrapErrorsReader{resp.Body}, w)

	var length int64
	if resp.ContentLength >= 0 {
		length = resp.ContentLength
	}

	ai, err := UploadArtifactWithOpts(ctx, cli, cacheArtifactNamespace, cachePath, teeR, UploadOpts{
		Labels: labels,
		Length: length,
	})
	if err != nil {
		return nil, CacheInfo{}, fmt.Errorf("failed to cache artifact: %w", err)
	}

	if opts.ExpectedSHA256 != "" && opts.ExpectedSHA256 != ai.DigestSHA256 {
		return nil, CacheInfo{}, status.Errorf(codes.FailedPrecondition, "artifact downloaded from source doesn't match expected digest: got %s, want %s", ai.DigestSHA256, opts.ExpectedSHA256)
	}

	if _, err := w.Seek(0, io.SeekStart); err != nil {
		return nil, CacheInfo{}, fmt.Errorf("failed to rewind the temp file: %w", err)
	}

	success = true
	return w, CacheInfo{CachedAt: ai.CreatedAt}, nil
}

type CacheSourceError struct {
	Err            error
	HTTPStatusCode int
}

func (err CacheSourceError) Error() string {
	return err.Err.Error()
}

func (err CacheSourceError) Unwrap() error {
	return err.Err
}

type wrapErrorsReader struct {
	io.Reader
}

func (r wrapErrorsReader) Read(p []byte) (n int, err error) {
	n, err = r.Reader.Read(p)
	if errors.Is(err, io.EOF) {
		// io.Copy checks for io.EOF equality
		return n, err
	}
	if err != nil {
		return n, CacheSourceError{err, 0}
	}
	return
}

func cacheArtifactPath(now time.Time, sourceURL *url.URL) string {
	safePath := slashRuns.ReplaceAllString(strings.Join(validPath.FindAllString(sourceURL.Path, -1), "-"), "/")

	p := now.Format("2006-01-02_15.04.05")
	p += "/" + sourceURL.Hostname()
	if safePath != "" {
		p += "/" + safePath
	}

	return p
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
