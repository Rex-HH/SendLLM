package service_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
	"sendllm/internal/service"
)

func TestImportAcceptsValidJSONLAndSkipsDuplicates(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	ensureTask(t, store, ctx)
	input := readFixture(t, "valid.jsonl")

	first, err := service.Import(ctx, store, "task-1", bytes.NewReader(input))
	if err != nil {
		t.Fatalf("Import(first) error = %v", err)
	}
	if first.Added != 2 || first.Skipped != 0 {
		t.Fatalf("Import(first) = %+v, want Added=2 Skipped=0", first)
	}

	second, err := service.Import(ctx, store, "task-1", bytes.NewReader(input))
	if err != nil {
		t.Fatalf("Import(second) error = %v", err)
	}
	if second.Added != 0 || second.Skipped != 2 {
		t.Fatalf("Import(second) = %+v, want Added=0 Skipped=2", second)
	}
}

func TestImportAcceptsCompactJSONL(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	ensureTask(t, store, ctx)
	input := `{"id":"dataset:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",` +
		`"source":{"dataset":"dataset","path":"source.json","index":1},` +
		`"messages":[{"role":"user","content":"compact prompt"},{"role":"assistant","content":"compact response"}],` +
		`"label":{"value":"safe"},"meta":{"sample_id":"sample-1"}}`

	stats, err := service.Import(ctx, store, "task-1", strings.NewReader(input+"\n"))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if stats.Added != 1 || stats.Skipped != 0 {
		t.Fatalf("Import() = %+v, want Added=1 Skipped=0", stats)
	}

	items, err := store.Claim(ctx, "task-1", 1, time.Now())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("claimed items = %d, want 1", len(items))
	}
	if items[0].TraceID != "dataset:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("TraceID = %q, want compact id", items[0].TraceID)
	}
	if items[0].Prompt != "compact prompt" || items[0].Response != "compact response" {
		t.Errorf("Prompt/Response = (%q, %q), want compact messages", items[0].Prompt, items[0].Response)
	}
}

func TestImportRejectsInvalidLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "empty line",
			input: "\n",
		},
		{
			name:  "invalid JSON",
			input: "not-json\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openTestStore(t)
			ctx := context.Background()
			ensureTask(t, store, ctx)

			_, err := service.Import(ctx, store, "task-1", strings.NewReader(tt.input))
			if !errors.Is(err, dto.ErrInvalidSource) {
				t.Fatalf("Import() error = %v, want %v", err, dto.ErrInvalidSource)
			}
			if !strings.Contains(err.Error(), "line 1") {
				t.Fatalf("Import() error = %v, want line number", err)
			}
		})
	}
}

func TestImportAcceptsLineAt16MiB(t *testing.T) {
	const exactLineSize = 16 * 1024 * 1024
	const prefix = `{"trace_id":"id-1","prompt":"`
	const suffix = `"}`
	line := prefix + strings.Repeat("x", exactLineSize-len(prefix)-len(suffix)) + suffix
	if len(line) != exactLineSize {
		t.Fatalf("line length = %d, want %d", len(line), exactLineSize)
	}

	store := openTestStore(t)
	ctx := context.Background()
	ensureTask(t, store, ctx)
	stats, err := service.Import(ctx, store, "task-1", strings.NewReader(line+"\n"))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if stats.Added != 1 || stats.Skipped != 0 {
		t.Fatalf("Import() = %+v, want Added=1 Skipped=0", stats)
	}
}

func TestImportRejectsLineLargerThan16MiB(t *testing.T) {
	const largestValidLineSize = 16 * 1024 * 1024
	const prefix = `{"trace_id":"id-1","prompt":"`
	const suffix = `"}`
	line := prefix + strings.Repeat("x", largestValidLineSize+1-len(prefix)-len(suffix)) + suffix
	if len(line) != largestValidLineSize+1 {
		t.Fatalf("line length = %d, want %d", len(line), largestValidLineSize+1)
	}

	store := openTestStore(t)
	ctx := context.Background()
	ensureTask(t, store, ctx)

	_, err := service.Import(ctx, store, "task-1", strings.NewReader(line+"\n"))
	if err == nil {
		t.Fatal("Import() error = nil, want oversized line error")
	}
	if !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("Import() error = %v, want line number", err)
	}
}

func TestImportConflictRollsBackBatch(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	ensureTask(t, store, ctx)
	seed, err := store.BeginImport(ctx, "task-1")
	if err != nil {
		t.Fatalf("BeginImport(seed) error = %v", err)
	}
	if _, err := seed.Add(ctx, dao.Item{
		TaskID:     "task-1",
		TraceID:    "existing",
		InputIndex: 2,
		RawJSON:    []byte(`{"trace_id":"existing","prompt":"old"}`),
		Prompt:     "old",
		State:      dao.ItemPending,
	}); err != nil {
		t.Fatalf("Add(seed) error = %v", err)
	}
	if err := seed.Commit(); err != nil {
		t.Fatalf("Commit(seed) error = %v", err)
	}

	_, err = service.Import(ctx, store, "task-1", strings.NewReader(
		"{\"trace_id\":\"new\",\"prompt\":\"new\"}\n{\"trace_id\":\"existing\",\"prompt\":\"changed\"}",
	))
	if !errors.Is(err, dao.ErrTraceConflict) {
		t.Fatalf("Import(conflict) error = %v, want %v", err, dao.ErrTraceConflict)
	}

	stats, err := service.Import(ctx, store, "task-1", strings.NewReader(`{"trace_id":"new","prompt":"new"}`))
	if err != nil {
		t.Fatalf("Import(rolled back item) error = %v", err)
	}
	if stats.Added != 1 {
		t.Fatalf("Import(rolled back item) = %+v, want Added=1", stats)
	}
}

func FuzzParseJSONL(f *testing.F) {
	f.Add([]byte("{\"trace_id\":\"id-1\",\"prompt\":\"ok\"}\n"))
	f.Add([]byte("not-json\n"))
	f.Fuzz(func(t *testing.T, give []byte) {
		store := openTestStore(t)
		ctx := context.Background()
		if err := store.EnsureTask(ctx, dao.Task{ID: "task-fuzz", SemanticHash: "hash-a"}); err != nil {
			t.Fatalf("EnsureTask() error = %v", err)
		}
		_, _ = service.Import(ctx, store, "task-fuzz", bytes.NewReader(give))
	})
}

func openTestStore(t *testing.T) *dao.Store {
	t.Helper()
	store, err := dao.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return store
}

func ensureTask(t *testing.T, store *dao.Store, ctx context.Context) {
	t.Helper()
	if err := store.EnsureTask(ctx, dao.Task{ID: "task-1", SemanticHash: "hash-a"}); err != nil {
		t.Fatalf("EnsureTask() error = %v", err)
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return contents
}
