package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
)

func TestRunner_ReportProgressPrefersTaskCancellationCause(t *testing.T) {
	store := openRunnerInternalStore(t)
	runner := &Runner{cfg: RunnerConfig{
		TaskID:     "task-1",
		Store:      store,
		OnProgress: func(Summary) {},
	}}
	providerErr := &dto.ProviderError{
		Kind:       dto.ProviderAuthentication,
		StatusCode: 401,
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(providerErr)

	err := runner.reportProgress(ctx, newProgressTracker(time.Now(), dao.Counts{}))
	var gotProviderErr *dto.ProviderError
	if !errors.As(err, &gotProviderErr) || gotProviderErr.Kind != dto.ProviderAuthentication {
		t.Fatalf("reportProgress() error = %v, want authentication ProviderError", err)
	}
}

func TestRunner_ClaimPrefersCancellationCause(t *testing.T) {
	store := openRunnerInternalStore(t)
	runner := &Runner{cfg: RunnerConfig{TaskID: "task-1", Store: store}}
	tests := []struct {
		name  string
		cause error
	}{
		{
			name: "task-level provider error",
			cause: &dto.ProviderError{
				Kind:       dto.ProviderAuthentication,
				StatusCode: 401,
			},
		},
		{name: "top-level user cancellation", cause: errors.New("user canceled task")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			cancel(test.cause)

			_, err := runner.claim(ctx, 1, time.Now())
			if !errors.Is(err, test.cause) {
				t.Fatalf("claim() error = %v, want cause %v", err, test.cause)
			}
		})
	}
}

func openRunnerInternalStore(t *testing.T) *dao.Store {
	t.Helper()
	store, err := dao.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("dao.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	if err := store.EnsureTask(
		context.Background(),
		dao.Task{ID: "task-1", SemanticHash: "test-hash"},
	); err != nil {
		t.Fatalf("EnsureTask() error = %v", err)
	}
	return store
}
