// Copyright 2022 Namespace Labs Inc; All rights reserved.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package sharded

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	const (
		shards      = 12
		concurrency = 3
	)

	seen := make([]atomic.Int64, shards)
	var active atomic.Int64
	var maxActive atomic.Int64
	err := Run(context.Background(), Options{
		ShardCount:       shards,
		Concurrency:      concurrency,
		AttemptsPerShard: 1,
	}, func(_ context.Context, shard int64) error {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			maximum := maxActive.Load()
			if current <= maximum || maxActive.CompareAndSwap(maximum, current) {
				break
			}
		}
		seen[shard].Add(1)
		time.Sleep(5 * time.Millisecond)
		return nil
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if got := maxActive.Load(); got < 2 || got > concurrency {
		t.Errorf("maximum concurrency = %d, want between 2 and %d", got, concurrency)
	}
	for shard := range seen {
		if got := seen[shard].Load(); got != 1 {
			t.Errorf("shard %d calls = %d, want 1", shard, got)
		}
	}
}

func TestRunRetries(t *testing.T) {
	var calls atomic.Int64
	var retries atomic.Int64
	err := Run(context.Background(), Options{
		ShardCount:       1,
		Concurrency:      1,
		AttemptsPerShard: 3,
		RetryDelay:       func(int) time.Duration { return 0 },
		OnRetry: func(shard int64, _ error, delay time.Duration) {
			if shard != 0 || delay != 0 {
				t.Errorf("unexpected retry: shard=%d delay=%v", shard, delay)
			}
			retries.Add(1)
		},
	}, func(context.Context, int64) error {
		if calls.Add(1) < 3 {
			return errors.New("retry")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("task calls = %d, want 3", got)
	}
	if got := retries.Load(); got != 2 {
		t.Errorf("retry callbacks = %d, want 2", got)
	}
}

func TestRunStopsAfterAttempts(t *testing.T) {
	wantErr := errors.New("failed")
	var calls atomic.Int64
	err := Run(context.Background(), Options{
		ShardCount:       10,
		Concurrency:      1,
		AttemptsPerShard: 3,
		RetryDelay:       func(int) time.Duration { return 0 },
	}, func(context.Context, int64) error {
		calls.Add(1)
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("task calls = %d, want 3", got)
	}
}

func TestDefaultRetryDelay(t *testing.T) {
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 10 * time.Second, 10 * time.Second}
	for i, wantDelay := range want {
		if got := defaultRetryDelay(i + 1); got != wantDelay {
			t.Errorf("defaultRetryDelay(%d) = %v, want %v", i+1, got, wantDelay)
		}
	}
}

func TestRunRejectsInvalidOptions(t *testing.T) {
	task := func(context.Context, int64) error { return nil }
	tests := []struct {
		name string
		opts Options
		task Task
	}{
		{name: "negative shards", opts: Options{ShardCount: -1}, task: task},
		{name: "negative concurrency", opts: Options{Concurrency: -1}, task: task},
		{name: "negative attempts", opts: Options{AttemptsPerShard: -1}, task: task},
		{name: "missing task"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := Run(context.Background(), test.opts, test.task); err == nil {
				t.Fatal("Run() succeeded, want error")
			}
		})
	}
}
