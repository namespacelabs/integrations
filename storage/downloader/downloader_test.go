// Copyright 2022 Namespace Labs Inc; All rights reserved.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package downloader

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	storageapi "namespacelabs.dev/integrations/api/storage"
	storagev1beta "namespacelabs.dev/integrations/proto/namespace/cloud/storage/v1beta"
)

func TestDownloadArtifact(t *testing.T) {
	const chunkSize = 64 * 1024
	data := testData(3 * chunkSize)
	httpServer := newRangeServer(t, data, nil)
	defer httpServer.Close()

	artifactServer := &testArtifactServer{url: httpServer.URL}
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	storagev1beta.RegisterArtifactsServiceServer(grpcServer, artifactServer)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient("passthrough:///storage", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	dest := filepath.Join(t.TempDir(), "artifact.bin")
	err = DownloadArtifact(context.Background(), storageapi.Client{
		Artifacts: storagev1beta.NewArtifactsServiceClient(conn),
		Conn:      conn,
	}, "main", "build/output", dest, Options{ChunkSize: chunkSize, Concurrent: 2, Resume: true})
	if err != nil {
		t.Fatalf("DownloadArtifact() error: %v", err)
	}

	assertFileContents(t, dest, data)
	assertPathMissing(t, dest+".state")
	assertPathMissing(t, dest+".download")
	if got := artifactServer.calls.Load(); got != 4 {
		t.Errorf("ResolveArtifact calls = %d, want 4", got)
	}
}

func TestDownloadParallelWithRetry(t *testing.T) {
	const chunkSize = 64 * 1024
	data := testData(12*chunkSize + 17)

	var failed atomic.Bool
	server := newRangeServer(t, data, func(start int64) int {
		if start == 2*chunkSize && !failed.Swap(true) {
			return http.StatusServiceUnavailable
		}
		return http.StatusPartialContent
	})
	defer server.Close()

	var retries atomic.Int64
	var downloaded atomic.Int64
	var resolved atomic.Int64
	dest := filepath.Join(t.TempDir(), "artifact.bin")
	err := Download(context.Background(), dest, Options{
		ChunkSize:  chunkSize,
		Concurrent: 4,
		ResolveURL: func(context.Context) (string, error) {
			resolved.Add(1)
			return server.URL, nil
		},
		OnProgress: func(current, _ int64) {
			downloaded.Store(current)
		},
		OnRetry: func(chunk int64, _ error, delay time.Duration) {
			if chunk != 2 || delay != 0 {
				t.Errorf("unexpected retry: chunk=%d delay=%v", chunk, delay)
			}
			retries.Add(1)
		},
		retryDelay: func(int) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}

	assertFileContents(t, dest, data)
	if got := retries.Load(); got != 1 {
		t.Errorf("retry count = %d, want 1", got)
	}
	if got := downloaded.Load(); got != int64(len(data)) {
		t.Errorf("downloaded bytes = %d, want %d", got, len(data))
	}
	if got := server.maxActive.Load(); got < 2 {
		t.Errorf("maximum concurrent requests = %d, want at least 2", got)
	}
	if got := resolved.Load(); got < 3 {
		t.Errorf("URL resolutions = %d, want at least 3", got)
	}
}

func TestDownloadResume(t *testing.T) {
	const chunkSize = 64 * 1024
	data := testData(6 * chunkSize)

	var fail atomic.Bool
	fail.Store(true)
	server := newRangeServer(t, data, func(start int64) int {
		if fail.Load() && start == 2*chunkSize {
			return http.StatusServiceUnavailable
		}
		return http.StatusPartialContent
	})
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "artifact.bin")
	oldContents := []byte("existing destination")
	if err := os.WriteFile(dest, oldContents, 0644); err != nil {
		t.Fatal(err)
	}

	opts := Options{
		ChunkSize:  chunkSize,
		Concurrent: 1,
		Resume:     true,
		ResolveURL: func(context.Context) (string, error) { return server.URL, nil },
		retryDelay: func(int) time.Duration { return 0 },
	}
	if err := Download(context.Background(), dest, opts); err == nil {
		t.Fatal("Download() succeeded, want retry exhaustion")
	}
	assertFileContents(t, dest, oldContents)
	assertPathExists(t, dest+".state")
	assertPathExists(t, dest+".download")

	requestsBeforeResume := server.requestsByStart()
	if requestsBeforeResume[0] != 1 || requestsBeforeResume[chunkSize] != 1 {
		t.Fatalf("completed chunk requests = %v, want one request each", requestsBeforeResume)
	}

	fail.Store(false)
	var firstProgress int64 = -1
	var progressOnce sync.Once
	opts.OnProgress = func(downloaded, _ int64) {
		progressOnce.Do(func() { firstProgress = downloaded })
	}
	if err := Download(context.Background(), dest, opts); err != nil {
		t.Fatalf("resumed Download() error: %v", err)
	}

	assertFileContents(t, dest, data)
	assertPathMissing(t, dest+".state")
	assertPathMissing(t, dest+".download")
	if firstProgress != 2*chunkSize {
		t.Errorf("initial resumed progress = %d, want %d", firstProgress, 2*chunkSize)
	}
	requestsAfterResume := server.requestsByStart()
	if requestsAfterResume[0] != 1 || requestsAfterResume[chunkSize] != 1 {
		t.Errorf("completed chunks were downloaded again: %v", requestsAfterResume)
	}
}

func TestDownloadSingleStream(t *testing.T) {
	data := testData(128 * 1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()

	var progress atomic.Int64
	dest := filepath.Join(t.TempDir(), "artifact.bin")
	err := Download(context.Background(), dest, Options{
		ChunkSize: 1,
		ResolveURL: func(context.Context) (string, error) {
			return server.URL, nil
		},
		OnProgress: func(downloaded, _ int64) { progress.Store(downloaded) },
	})
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}

	assertFileContents(t, dest, data)
	if got := progress.Load(); got != int64(len(data)) {
		t.Errorf("downloaded bytes = %d, want %d", got, len(data))
	}
}

func TestDownloadRestartsInvalidResumeState(t *testing.T) {
	const chunkSize = 64 * 1024
	data := testData(3 * chunkSize)
	server := newRangeServer(t, data, nil)
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "artifact.bin")
	digest := sha256.Sum256(data[:chunkSize])
	state := []byte(fmt.Sprintf(`{"version":1,"chunk_size":65536,"chunks_done":[0],"digests":{"0":"sha256:%s"},"downloaded_bytes":65536}`, hex.EncodeToString(digest[:])))
	if err := os.WriteFile(dest+".state", state, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest+".download", make([]byte, len(data)), 0644); err != nil {
		t.Fatal(err)
	}

	err := Download(context.Background(), dest, Options{
		ChunkSize:  chunkSize,
		Concurrent: 2,
		Resume:     true,
		ResolveURL: func(context.Context) (string, error) { return server.URL, nil },
	})
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}

	assertFileContents(t, dest, data)
	if got := server.requestsByStart()[0]; got != 1 {
		t.Errorf("first chunk requests = %d, want 1 after invalid state restart", got)
	}
}

func TestDownloadRejectsInvalidOptions(t *testing.T) {
	resolve := func(context.Context) (string, error) { return "", nil }
	tests := []struct {
		name string
		opts Options
	}{
		{name: "missing resolver"},
		{name: "negative chunk size", opts: Options{ChunkSize: -1, ResolveURL: resolve}},
		{name: "negative concurrency", opts: Options{Concurrent: -1, ResolveURL: resolve}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := Download(context.Background(), filepath.Join(t.TempDir(), "artifact.bin"), test.opts); err == nil {
				t.Fatal("Download() succeeded, want error")
			}
		})
	}
}

type rangeServer struct {
	*httptest.Server
	data []byte

	active    atomic.Int64
	maxActive atomic.Int64

	mu       sync.Mutex
	requests map[int64]int
}

type testArtifactServer struct {
	storagev1beta.UnimplementedArtifactsServiceServer
	url   string
	calls atomic.Int64
}

func (s *testArtifactServer) ResolveArtifact(_ context.Context, req *storagev1beta.ResolveArtifactRequest) (*storagev1beta.ResolveArtifactResponse, error) {
	if req.GetNamespace() != "main" || req.GetPath() != "build/output" {
		return nil, fmt.Errorf("unexpected artifact: %s/%s", req.GetNamespace(), req.GetPath())
	}
	s.calls.Add(1)
	return &storagev1beta.ResolveArtifactResponse{SignedDownloadUrl: s.url}, nil
}

func newRangeServer(t *testing.T, data []byte, status func(start int64) int) *rangeServer {
	t.Helper()
	s := &rangeServer{data: data, requests: map[int64]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		if r.Method == http.MethodHead {
			return
		}

		var start, end int64
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
			http.Error(w, "invalid range", http.StatusBadRequest)
			return
		}
		if start < 0 || end < start || end >= int64(len(data)) {
			http.Error(w, "range out of bounds", http.StatusRequestedRangeNotSatisfiable)
			return
		}

		s.mu.Lock()
		s.requests[start]++
		s.mu.Unlock()

		active := s.active.Add(1)
		defer s.active.Add(-1)
		for {
			maximum := s.maxActive.Load()
			if active <= maximum || s.maxActive.CompareAndSwap(maximum, active) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)

		if statusCode := http.StatusPartialContent; status != nil {
			statusCode = status(start)
			if statusCode != http.StatusPartialContent {
				http.Error(w, http.StatusText(statusCode), statusCode)
				return
			}
		}

		body := data[start : end+1]
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body)
	}))
	return s
}

func (s *rangeServer) requestsByStart() map[int64]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	requests := make(map[int64]int, len(s.requests))
	for start, count := range s.requests {
		requests[start] = count
	}
	return requests
}

func testData(size int) []byte {
	pattern := []byte("namespace-artifact-")
	return bytes.Repeat(pattern, (size+len(pattern)-1)/len(pattern))[:size]
}

func assertFileContents(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("contents of %s do not match", path)
	}
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("%s should exist: %v", path, err)
	}
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("%s should not exist: %v", path, err)
	}
}
