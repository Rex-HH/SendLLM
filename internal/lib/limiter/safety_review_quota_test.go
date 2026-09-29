package limiter

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

// TestSafetyReviewQuotaRoleConcurrency 验证角色并发上限生效。
func TestSafetyReviewQuotaRoleConcurrency(t *testing.T) {
	quota, err := NewSafetyReviewQuota(SafetyReviewQuotaConfig{
		Roles: map[string]SafetyReviewRoleQuota{
			"judge_a": {Concurrency: 1},
		},
	})
	if err != nil {
		t.Fatalf("NewSafetyReviewQuota() error = %v", err)
	}
	release, err := quota.Acquire(context.Background(), "judge_a", "", 0)
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := quota.Acquire(ctx, "judge_a", "", 0); err == nil {
		t.Fatal("second Acquire() returned before role release")
	}
	release()
	if _, err := quota.Acquire(context.Background(), "judge_a", "", 0); err != nil {
		t.Fatalf("Acquire(after release) error = %v", err)
	}
}

// TestSafetyReviewQuotaGroupConcurrency 验证共享组并发上限生效。
func TestSafetyReviewQuotaGroupConcurrency(t *testing.T) {
	quota, err := NewSafetyReviewQuota(SafetyReviewQuotaConfig{
		Roles: map[string]SafetyReviewRoleQuota{
			"judge_a": {Concurrency: 2},
			"judge_b": {Concurrency: 2},
		},
		Groups: map[string]SafetyReviewGroupQuota{
			"shared": {Concurrency: 1},
		},
	})
	if err != nil {
		t.Fatalf("NewSafetyReviewQuota() error = %v", err)
	}
	release, err := quota.Acquire(context.Background(), "judge_a", "shared", 0)
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := quota.Acquire(ctx, "judge_b", "shared", 0); err == nil {
		t.Fatal("second Acquire() returned before group release")
	}
	release()
	if _, err := quota.Acquire(context.Background(), "judge_b", "shared", 0); err != nil {
		t.Fatalf("Acquire(after release) error = %v", err)
	}
}

// TestSafetyReviewQuotaRates 验证 RPM 与 TPM 限制。
func TestSafetyReviewQuotaRates(t *testing.T) {
	quota, err := NewSafetyReviewQuota(SafetyReviewQuotaConfig{
		Roles: map[string]SafetyReviewRoleQuota{
			"judge_a": {Concurrency: 2, RequestsPerMinute: 1, TokensPerMinute: 10},
		},
	})
	if err != nil {
		t.Fatalf("NewSafetyReviewQuota() error = %v", err)
	}
	if _, err := quota.Acquire(context.Background(), "judge_a", "", 11); !errors.Is(err, ErrTokenBudgetExceeded) {
		t.Fatalf("Acquire(above token budget) error = %v, want %v", err, ErrTokenBudgetExceeded)
	}
	release, err := quota.Acquire(context.Background(), "judge_a", "", 10)
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
	release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := quota.Acquire(ctx, "judge_a", "", 10); err == nil {
		t.Fatal("second Acquire() returned before RPM refill")
	}
}

// TestSafetyReviewQuotaExpertPriority 验证 Expert 优先于 Router。
func TestSafetyReviewQuotaExpertPriority(t *testing.T) {
	quota, err := NewSafetyReviewQuota(SafetyReviewQuotaConfig{
		Roles: map[string]SafetyReviewRoleQuota{
			"router": {Concurrency: 1},
			"expert": {Concurrency: 1},
		},
		Groups: map[string]SafetyReviewGroupQuota{
			"shared": {Concurrency: 1},
		},
	})
	if err != nil {
		t.Fatalf("NewSafetyReviewQuota() error = %v", err)
	}
	holder, err := quota.Acquire(context.Background(), "judge_a", "shared", 0)
	if err != nil {
		t.Fatalf("holder Acquire() error = %v", err)
	}
	order := make(chan string, 2)
	errors := make(chan error, 2)
	go func() {
		release, acquireErr := quota.Acquire(context.Background(), "router", "shared", 0)
		if acquireErr != nil {
			errors <- acquireErr
			return
		}
		order <- "router"
		if release != nil {
			release()
		}
		errors <- nil
	}()
	go func() {
		release, acquireErr := quota.Acquire(context.Background(), "expert", "shared", 0)
		if acquireErr != nil {
			errors <- acquireErr
			return
		}
		order <- "expert"
		if release != nil {
			release()
		}
		errors <- nil
	}()
	waitForSafetyReviewWaiting(t, quota, 2)
	holder()
	select {
	case role := <-order:
		if role != "expert" {
			t.Fatalf("first acquired role = %s, want expert", role)
		}
	case err := <-errors:
		if err != nil {
			t.Fatalf("Acquire() error = %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expert acquisition timed out")
	}
	if err := <-errors; err != nil {
		t.Fatalf("second Acquire() error = %v", err)
	}
}

// TestSafetyReviewQuotaFIFO 验证同优先级按到达顺序获取许可。
func TestSafetyReviewQuotaFIFO(t *testing.T) {
	quota, err := NewSafetyReviewQuota(SafetyReviewQuotaConfig{
		Roles: map[string]SafetyReviewRoleQuota{
			"judge_a": {Concurrency: 1},
			"judge_b": {Concurrency: 1},
		},
		Groups: map[string]SafetyReviewGroupQuota{
			"shared": {Concurrency: 1},
		},
	})
	if err != nil {
		t.Fatalf("NewSafetyReviewQuota() error = %v", err)
	}
	holder, err := quota.Acquire(context.Background(), "router", "shared", 0)
	if err != nil {
		t.Fatalf("holder Acquire() error = %v", err)
	}
	order := make(chan string, 2)
	errors := make(chan error, 2)
	go func() {
		release, acquireErr := quota.Acquire(context.Background(), "judge_a", "shared", 0)
		if acquireErr != nil {
			errors <- acquireErr
			return
		}
		order <- "judge_a"
		if release != nil {
			release()
		}
		errors <- nil
	}()
	waitForSafetyReviewWaiting(t, quota, 1)
	go func() {
		release, acquireErr := quota.Acquire(context.Background(), "judge_b", "shared", 0)
		if acquireErr != nil {
			errors <- acquireErr
			return
		}
		order <- "judge_b"
		if release != nil {
			release()
		}
		errors <- nil
	}()
	waitForSafetyReviewWaiting(t, quota, 2)
	holder()
	select {
	case role := <-order:
		if role != "judge_a" {
			t.Fatalf("first acquired role = %s, want judge_a", role)
		}
	case err := <-errors:
		if err != nil {
			t.Fatalf("Acquire() error = %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("first acquisition timed out")
	}
	if err := <-errors; err != nil {
		t.Fatalf("second Acquire() error = %v", err)
	}
}

// waitForSafetyReviewWaiting 等待共享组出现指定数量的等待者。
func waitForSafetyReviewWaiting(t *testing.T, quota *SafetyReviewQuota, want int) {
	t.Helper()
	deadline := time.Now().Add(200 * time.Millisecond)
	for quota.waiting() < want {
		if time.Now().After(deadline) {
			t.Fatalf("waiting count = %d, want at least %d", quota.waiting(), want)
		}
		runtime.Gosched()
	}
}

// TestSafetyReviewQuotaCancelAndDoubleRelease 验证取消不泄露许可且释放幂等。
func TestSafetyReviewQuotaCancelAndDoubleRelease(t *testing.T) {
	quota, err := NewSafetyReviewQuota(SafetyReviewQuotaConfig{
		Roles: map[string]SafetyReviewRoleQuota{
			"judge_a": {Concurrency: 1},
		},
	})
	if err != nil {
		t.Fatalf("NewSafetyReviewQuota() error = %v", err)
	}
	first, err := quota.Acquire(context.Background(), "judge_a", "", 0)
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := quota.Acquire(ctx, "judge_a", "", 0); err == nil {
		t.Fatal("canceled Acquire() returned nil error")
	}
	first()
	first()
	second, err := quota.Acquire(context.Background(), "judge_a", "", 0)
	if err != nil {
		t.Fatalf("second Acquire() error = %v", err)
	}
	second()
}
