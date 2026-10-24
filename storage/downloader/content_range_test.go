// Copyright 2026 Namespace Labs Inc; All rights reserved.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package downloader

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDownloadRejectsInvalidContentRange(t *testing.T) {
	const chunkSize = 4
	data := []byte("AAAABBBB")
	tests := []struct {
		name  string
		value string
	}{
		{name: "missing"},
		{name: "malformed", value: "not a range"},
		{name: "wrong unit", value: "items 0-3/8"},
		{name: "wrong start", value: "bytes 4-7/8"},
		{name: "wrong end", value: "bytes 0-2/8"},
		{name: "wrong total", value: "bytes 0-3/9"},
		{name: "unsatisfied", value: "bytes */8"},
		{name: "negative offset", value: "bytes -0-3/8"},
		{name: "signed offset", value: "bytes +0-3/8"},
		{name: "overflow", value: "bytes 0-3/18446744073709551616"},
		{name: "trailing data", value: "bytes 0-3/8 garbage"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Accept-Ranges", "bytes")
				if r.Method == http.MethodHead {
					w.Header().Set("Content-Length", fmt.Sprint(len(data)))
					return
				}
				w.Header().Set("Content-Range", test.value)
				w.WriteHeader(http.StatusPartialContent)
				// The body has the expected length but contains the wrong bytes.
				_, _ = w.Write(data[chunkSize:])
			}))
			defer server.Close()

			dest := filepath.Join(t.TempDir(), "artifact.bin")
			original := []byte("existing destination")
			if err := os.WriteFile(dest, original, 0644); err != nil {
				t.Fatal(err)
			}
			err := Download(context.Background(), dest, Options{
				ChunkSize:  chunkSize,
				Concurrent: 1,
				ResolveURL: func(context.Context) (string, error) { return server.URL, nil },
				retryDelay: func(int) time.Duration { return 0 },
			})
			if err == nil || !strings.Contains(err.Error(), "Content-Range") {
				t.Fatalf("Download() error = %v, want Content-Range error", err)
			}
			assertFileContents(t, dest, original)
			assertPathMissing(t, filepath.Join(filepath.Dir(dest), ".artifact.bin.tmp"))
		})
	}
}

func TestDownloadRetriesInvalidContentRange(t *testing.T) {
	const chunkSize = 4
	data := []byte("AAAABBBBCC")
	for _, unknownTotal := range []bool{false, true} {
		t.Run(fmt.Sprintf("unknown_total=%t", unknownTotal), func(t *testing.T) {
			var requested atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Accept-Ranges", "bytes")
				if r.Method == http.MethodHead {
					w.Header().Set("Content-Length", fmt.Sprint(len(data)))
					return
				}
				if !requested.Swap(true) {
					w.Header().Set("Content-Range", "bytes 4-7/10")
					w.WriteHeader(http.StatusPartialContent)
					_, _ = w.Write(data[4:8])
					return
				}
				var start, end int
				if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
					t.Error(err)
					return
				}
				if unknownTotal {
					w.Header().Set("Content-Range", fmt.Sprintf("BYTES %03d-%03d/*", start, end))
				} else {
					w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
				}
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(data[start : end+1])
			}))
			defer server.Close()

			dest := filepath.Join(t.TempDir(), "artifact.bin")
			retries := 0
			err := Download(context.Background(), dest, Options{
				ChunkSize:  chunkSize,
				Concurrent: 1,
				Resume:     true,
				ResolveURL: func(context.Context) (string, error) { return server.URL, nil },
				retryDelay: func(int) time.Duration { return 0 },
				OnRetry: func(chunk int64, err error, _ time.Duration) {
					if chunk != 0 || !strings.Contains(err.Error(), "Content-Range") {
						t.Errorf("unexpected retry: chunk=%d error=%v", chunk, err)
					}
					retries++
				},
			})
			if err != nil {
				t.Fatalf("Download() error: %v", err)
			}
			if retries != 1 {
				t.Errorf("retries = %d, want 1", retries)
			}
			assertFileContents(t, dest, data)
			assertPathMissing(t, dest+".state")
			assertPathMissing(t, dest+".download")
		})
	}
}

func TestDownloadInvalidContentRangePreservesResumeState(t *testing.T) {
	const chunkSize = 4
	data := []byte("AAAABBBB")
	var valid atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			return
		}
		if !valid.Load() {
			w.Header().Set("Content-Range", "bytes 4-7/8")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(data[4:])
			return
		}
		var start, end int
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/8", start, end))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[start : end+1])
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "artifact.bin")
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
	assertPathMissing(t, dest)
	assertFileContents(t, dest+".download", make([]byte, len(data)))
	saved, err := os.ReadFile(dest + ".state")
	if err != nil {
		t.Fatal(err)
	}
	var state downloadState
	if err := json.Unmarshal(saved, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.ChunksDone) != 0 || state.DownloadedBytes != 0 || len(state.Digests) != 0 {
		t.Fatalf("invalid response was recorded as complete: %+v", state)
	}

	valid.Store(true)
	if err := Download(context.Background(), dest, opts); err != nil {
		t.Fatalf("resumed Download() error: %v", err)
	}
	assertFileContents(t, dest, data)
	assertPathMissing(t, dest+".state")
	assertPathMissing(t, dest+".download")
}
