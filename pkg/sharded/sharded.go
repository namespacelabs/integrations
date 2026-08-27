// Copyright 2022 Namespace Labs Inc; All rights reserved.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package sharded

import (
	"context"
	"errors"
	"sync"
	"time"
)

type Options struct {
	// ShardCount is the number of zero-based shards to execute.
	ShardCount int64
	// Concurrency is the maximum number of shards in flight. The default is 1.
	Concurrency int
	// AttemptsPerShard includes the initial attempt. The default is 1.
	AttemptsPerShard int

	// RetryDelay receives the failed attempt number, starting at 1. By default,
	// retries use exponential backoff capped at 10 seconds.
	RetryDelay func(attempt int) time.Duration

	// OnRetry may be called concurrently before a failed shard is retried.
	OnRetry func(shard int64, err error, delay time.Duration)
}

// Task executes one zero-based shard.
type Task func(context.Context, int64) error

// Run executes a task for each shard with bounded concurrency and retries.
// It cancels the remaining work when any shard exhausts its attempts.
func Run(ctx context.Context, opts Options, task Task) error {
	if opts.ShardCount < 0 {
		return errors.New("shard count must not be negative")
	}
	if opts.Concurrency == 0 {
		opts.Concurrency = 1
	}
	if opts.Concurrency < 0 {
		return errors.New("concurrency must be positive")
	}
	if opts.AttemptsPerShard == 0 {
		opts.AttemptsPerShard = 1
	}
	if opts.AttemptsPerShard < 0 {
		return errors.New("attempts per shard must be positive")
	}
	if task == nil {
		return errors.New("task is required")
	}
	if opts.RetryDelay == nil {
		opts.RetryDelay = defaultRetryDelay
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	semaphore := make(chan struct{}, opts.Concurrency)
	var wg sync.WaitGroup
	var errOnce sync.Once
	var firstErr error
	var schedulingErr error

schedule:
	for shard := int64(0); shard < opts.ShardCount; shard++ {
		select {
		case semaphore <- struct{}{}:
			if err := ctx.Err(); err != nil {
				<-semaphore
				schedulingErr = err
				break schedule
			}
		case <-ctx.Done():
			schedulingErr = ctx.Err()
			break schedule
		}

		wg.Add(1)
		go func(shard int64) {
			defer wg.Done()
			defer func() { <-semaphore }()

			if err := runWithRetries(ctx, shard, opts, task); err != nil {
				errOnce.Do(func() {
					firstErr = err
					cancel()
				})
			}
		}(shard)
	}

	wg.Wait()
	if firstErr == nil {
		return schedulingErr
	}
	return firstErr
}

func runWithRetries(ctx context.Context, shard int64, opts Options, task Task) error {
	for attempt := 1; attempt <= opts.AttemptsPerShard; attempt++ {
		err := task(ctx, shard)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if attempt == opts.AttemptsPerShard {
			return err
		}

		delay := opts.RetryDelay(attempt)
		if opts.OnRetry != nil {
			opts.OnRetry(shard, err, delay)
		}
		if err := wait(ctx, delay); err != nil {
			return err
		}
	}

	return nil
}

func defaultRetryDelay(attempt int) time.Duration {
	if attempt >= 5 {
		return 10 * time.Second
	}
	return time.Duration(1<<uint(attempt-1)) * time.Second
}

func wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
