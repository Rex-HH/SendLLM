package limiter_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"sendllm/internal/lib/limiter"
)

func TestLimiterConcurrencyWaitsForRelease(t *testing.T) {
	value, err := limiter.New(limiter.Config{Concurrency: 1})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	firstRelease, err := value.Acquire(context.Background(), 0)
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}

	secondDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()

		release, acquireErr := value.Acquire(ctx, 0)
		if acquireErr == nil {
			release()
		}
		secondDone <- acquireErr
	}()

	select {
	case err := <-secondDone:
		t.Fatalf("second Acquire() returned before release: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	firstRelease()
	if err := <-secondDone; err != nil {
		t.Fatalf("second Acquire() error = %v", err)
	}
}

func TestLimiterCooldownWaits(t *testing.T) {
	value, err := limiter.New(limiter.Config{Concurrency: 1})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	value.Cooldown(time.Now().Add(35 * time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	started := time.Now()
	release, err := value.Acquire(ctx, 0)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	defer release()
	if elapsed := time.Since(started); elapsed < 20*time.Millisecond {
		t.Errorf("Acquire() returned after %v, want at least 20ms cooldown", elapsed)
	}
}

func TestLimiterZeroRatesDoNotBlock(t *testing.T) {
	value, err := limiter.New(limiter.Config{Concurrency: 2})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	release, err := value.Acquire(ctx, 1_000_000)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	defer release()
}

func TestLimiterRejectsEstimateAboveTokenBudget(t *testing.T) {
	value, err := limiter.New(limiter.Config{
		Concurrency:     1,
		TokensPerMinute: 10,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = value.Acquire(context.Background(), 11)
	if !errors.Is(err, limiter.ErrTokenBudgetExceeded) {
		t.Fatalf("Acquire() error = %v, want %v", err, limiter.ErrTokenBudgetExceeded)
	}
}

func TestLimiterReleaseIsIdempotent(t *testing.T) {
	value, err := limiter.New(limiter.Config{Concurrency: 1})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	release, err := value.Acquire(context.Background(), 0)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	release()
	release()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	secondRelease, err := value.Acquire(ctx, 0)
	if err != nil {
		t.Fatalf("second Acquire() error = %v", err)
	}
	secondRelease()
}
