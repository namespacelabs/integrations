// Copyright 2022 Namespace Labs Inc; All rights reserved.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"namespacelabs.dev/integrations/pkg/sharded"
)

const (
	defaultChunkSize  = 4 * 1024 * 1024
	defaultConcurrent = 4
	attemptsPerChunk  = 10
	localStateVersion = 1
)

type Options struct {
	// ChunkSize is the size of each ranged request. The default is 4 MiB.
	ChunkSize int64
	// Concurrent is the maximum number of requests in flight. The default is 4.
	Concurrent int
	// Resume retains partial data and completed-chunk state after a failure.
	Resume bool

	// ResolveURL is called for each request so callers can refresh expiring URLs.
	// It may be called concurrently.
	ResolveURL func(context.Context) (string, error)

	// HTTPClient defaults to http.DefaultClient.
	HTTPClient *http.Client

	// OnProgress may be called concurrently. A resumed download begins with a
	// call containing the number of bytes completed in the previous attempt.
	OnProgress func(downloaded, total int64)

	// OnRetry may be called concurrently before a failed chunk is retried.
	OnRetry func(chunk int64, err error, delay time.Duration)

	retryDelay func(attempt int) time.Duration
}

type downloadState struct {
	Version         int              `json:"version"`
	ChunkSize       int64            `json:"chunk_size"`
	ChunksDone      []int64          `json:"chunks_done"`
	Digests         map[int64]string `json:"digests,omitempty"`
	DownloadedBytes int64            `json:"downloaded_bytes"`
}

type localState struct {
	stateFile string
	state     *downloadState
	mu        sync.Mutex
}

// Download writes the resolved URL's contents to destPath. Servers that
// advertise byte ranges are downloaded in parallel; others use a single GET.
func Download(ctx context.Context, destPath string, opts Options) error {
	if opts.ChunkSize == 0 {
		opts.ChunkSize = defaultChunkSize
	}
	if opts.ChunkSize < 0 {
		return errors.New("chunk size must be positive")
	}
	if opts.Concurrent == 0 {
		opts.Concurrent = defaultConcurrent
	}
	if opts.Concurrent < 0 {
		return errors.New("concurrent downloads must be positive")
	}
	if opts.ResolveURL == nil {
		return errors.New("URL resolver is required")
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
	}

	contentLength, acceptsRanges, err := inspectSource(ctx, opts)
	if err != nil {
		return err
	}
	if contentLength <= 0 || contentLength < opts.ChunkSize || !acceptsRanges {
		return downloadSingleStream(ctx, destPath, opts)
	}

	stateFile := ""
	downloadFile := filepath.Join(filepath.Dir(destPath), "."+filepath.Base(destPath)+".tmp")
	if opts.Resume {
		stateFile = destPath + ".state"
		downloadFile = destPath + ".download"
	}

	state, isNew, err := loadState(stateFile, opts.ChunkSize)
	if err != nil {
		return fmt.Errorf("load download state: %w", err)
	}

	totalChunks := 1 + (contentLength-1)/opts.ChunkSize
	if !isNew {
		valid, err := state.valid(downloadFile, contentLength, totalChunks)
		if err != nil {
			return fmt.Errorf("validate partial download: %w", err)
		}
		if !valid {
			state = newState(stateFile, opts.ChunkSize)
			isNew = true
		}
	}

	if isNew {
		if err := state.save(); err != nil {
			return fmt.Errorf("save initial download state: %w", err)
		}
	}

	flags := os.O_CREATE | os.O_RDWR
	if isNew {
		flags |= os.O_TRUNC
	}
	output, err := os.OpenFile(downloadFile, flags, 0644)
	if err != nil {
		return fmt.Errorf("open partial download: %w", err)
	}

	if isNew {
		if err := output.Truncate(contentLength); err != nil {
			output.Close()
			return fmt.Errorf("allocate partial download: %w", err)
		}
	}

	var downloadedBytes atomic.Int64
	downloadedBytes.Store(state.downloadedBytes())
	if opts.OnProgress != nil {
		opts.OnProgress(downloadedBytes.Load(), contentLength)
	}

	err = sharded.Run(ctx, sharded.Options{
		ShardCount:       totalChunks,
		Concurrency:      opts.Concurrent,
		AttemptsPerShard: attemptsPerChunk,
		RetryDelay:       opts.retryDelay,
		OnRetry:          opts.OnRetry,
	}, func(ctx context.Context, chunk int64) error {
		if state.isFinished(chunk) {
			return nil
		}

		written, digest, err := downloadChunk(ctx, output, chunk, contentLength, opts)
		if err != nil {
			return err
		}
		if err := state.finishedChunk(chunk, written, digest); err != nil {
			return fmt.Errorf("save completed chunk: %w", err)
		}

		downloaded := downloadedBytes.Add(written)
		if opts.OnProgress != nil {
			opts.OnProgress(downloaded, contentLength)
		}
		return nil
	})
	if err != nil {
		output.Close()
		if !opts.Resume {
			_ = os.Remove(downloadFile)
		}
		return err
	}

	if err := output.Close(); err != nil {
		return fmt.Errorf("close partial download: %w", err)
	}
	if err := os.Rename(downloadFile, destPath); err != nil {
		return fmt.Errorf("install downloaded file: %w", err)
	}
	if err := state.completed(); err != nil {
		return fmt.Errorf("remove download state: %w", err)
	}

	return nil
}

func inspectSource(ctx context.Context, opts Options) (int64, bool, error) {
	url, err := opts.ResolveURL(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("resolve download URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0, false, fmt.Errorf("create HEAD request: %w", err)
	}
	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		return 0, false, fmt.Errorf("inspect download: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, false, fmt.Errorf("inspect download: unexpected HTTP status %d", resp.StatusCode)
	}

	acceptsRanges := strings.EqualFold(strings.TrimSpace(resp.Header.Get("Accept-Ranges")), "bytes")
	return resp.ContentLength, acceptsRanges, nil
}

func downloadChunk(ctx context.Context, output *os.File, chunk, totalSize int64, opts Options) (int64, string, error) {
	start := chunk * opts.ChunkSize
	chunkSize := min(opts.ChunkSize, totalSize-start)
	end := start + chunkSize - 1

	url, err := opts.ResolveURL(ctx)
	if err != nil {
		return 0, "", fmt.Errorf("resolve download URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, "", fmt.Errorf("create chunk request: %w", err)
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("download chunk %d: %w", chunk, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return 0, "", fmt.Errorf("download chunk %d: unexpected HTTP status %d", chunk, resp.StatusCode)
	}

	hasher := sha256.New()
	w := io.MultiWriter(io.NewOffsetWriter(output, start), hasher)
	written, err := io.Copy(w, io.LimitReader(resp.Body, chunkSize))
	if err != nil {
		return 0, "", fmt.Errorf("download chunk %d: %w", chunk, err)
	}
	if written != chunkSize {
		return 0, "", fmt.Errorf("download chunk %d: expected %d bytes, got %d", chunk, chunkSize, written)
	}

	return written, "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}

func downloadSingleStream(ctx context.Context, destPath string, opts Options) (retErr error) {
	url, err := opts.ResolveURL(ctx)
	if err != nil {
		return fmt.Errorf("resolve download URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("download file: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download file: unexpected HTTP status %d", resp.StatusCode)
	}

	tmpPath := filepath.Join(filepath.Dir(destPath), "."+filepath.Base(destPath)+".tmp")
	output, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("create partial download: %w", err)
	}
	defer func() {
		if retErr != nil {
			output.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	var downloaded int64
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			written, writeErr := output.Write(buf[:n])
			if writeErr != nil {
				return fmt.Errorf("write partial download: %w", writeErr)
			}
			if written != n {
				return io.ErrShortWrite
			}
			downloaded += int64(written)
			if opts.OnProgress != nil {
				opts.OnProgress(downloaded, resp.ContentLength)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return fmt.Errorf("read download: %w", readErr)
		}
	}

	if err := output.Close(); err != nil {
		return fmt.Errorf("close partial download: %w", err)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("install downloaded file: %w", err)
	}
	return nil
}

func newState(stateFile string, chunkSize int64) *localState {
	return &localState{
		stateFile: stateFile,
		state: &downloadState{
			Version:    localStateVersion,
			ChunkSize:  chunkSize,
			ChunksDone: []int64{},
		},
	}
}

func loadState(stateFile string, chunkSize int64) (*localState, bool, error) {
	if stateFile == "" {
		return newState("", chunkSize), true, nil
	}

	data, err := os.ReadFile(stateFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return newState(stateFile, chunkSize), true, nil
		}
		return nil, false, err
	}

	var state downloadState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, false, err
	}
	if state.Version != localStateVersion || state.ChunkSize != chunkSize {
		return newState(stateFile, chunkSize), true, nil
	}

	return &localState{stateFile: stateFile, state: &state}, false, nil
}

func (s *localState) valid(downloadFile string, totalSize, totalChunks int64) (bool, error) {
	file, err := os.Open(downloadFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if info.Size() != totalSize {
		return false, nil
	}

	seen := make(map[int64]struct{}, len(s.state.ChunksDone))
	var downloaded int64
	for _, chunk := range s.state.ChunksDone {
		if chunk < 0 || chunk >= totalChunks {
			return false, nil
		}
		if _, ok := seen[chunk]; ok {
			return false, nil
		}
		seen[chunk] = struct{}{}

		chunkSize := min(s.state.ChunkSize, totalSize-chunk*s.state.ChunkSize)
		downloaded += chunkSize
		if want := s.state.Digests[chunk]; want != "" {
			hasher := sha256.New()
			if _, err := io.Copy(hasher, io.NewSectionReader(file, chunk*s.state.ChunkSize, chunkSize)); err != nil {
				return false, err
			}
			if got := "sha256:" + hex.EncodeToString(hasher.Sum(nil)); got != want {
				return false, nil
			}
		}
	}

	return downloaded == s.state.DownloadedBytes, nil
}

func (s *localState) isFinished(chunk int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Contains(s.state.ChunksDone, chunk)
}

func (s *localState) downloadedBytes() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.DownloadedBytes
}

func (s *localState) finishedChunk(chunk, chunkBytes int64, digest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	previousChunks := slices.Clone(s.state.ChunksDone)
	previousBytes := s.state.DownloadedBytes
	previousDigest, hadDigest := s.state.Digests[chunk]

	s.state.ChunksDone = append(s.state.ChunksDone, chunk)
	slices.Sort(s.state.ChunksDone)
	s.state.DownloadedBytes += chunkBytes
	if digest != "" {
		if s.state.Digests == nil {
			s.state.Digests = map[int64]string{}
		}
		s.state.Digests[chunk] = digest
	}

	if err := s.saveLocked(); err != nil {
		s.state.ChunksDone = previousChunks
		s.state.DownloadedBytes = previousBytes
		if hadDigest {
			s.state.Digests[chunk] = previousDigest
		} else {
			delete(s.state.Digests, chunk)
		}
		return err
	}
	return nil
}

func (s *localState) save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *localState) saveLocked() error {
	if s.stateFile == "" {
		return nil
	}
	data, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	tmpPath := s.stateFile + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.stateFile)
}

func (s *localState) completed() error {
	if s.stateFile == "" {
		return nil
	}
	return os.Remove(s.stateFile)
}
