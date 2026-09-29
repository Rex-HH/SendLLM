package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"sendllm/internal/dao"
)

// TestSafetyReviewStatusSnapshot 验证状态摘要、吞吐、ETA 和错误聚合。
func TestSafetyReviewStatusSnapshot(t *testing.T) {
	store, taskID, _ := newSafetyReviewExportStore(t)
	reporter, err := NewSafetyReviewStatusReporter(SafetyReviewStatusConfig{
		TaskID: taskID, Store: store, Interval: time.Second,
	})
	if err != nil {
		t.Fatalf("NewSafetyReviewStatusReporter() error = %v", err)
	}
	snapshot, err := reporter.Snapshot(context.Background(), taskID)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.TaskID != taskID || snapshot.Scene != "response" {
		t.Fatalf("snapshot identity = %#v", snapshot)
	}
	if snapshot.Items["resolved_unsafe"] != 1 || snapshot.Items["quarantined"] != 1 ||
		snapshot.Decisions != 2 {
		t.Fatalf("snapshot counts = %#v", snapshot)
	}
	if snapshot.Stages["succeeded"] != 4 || snapshot.Stages["pending"] != 7 {
		t.Fatalf("stage counts = %#v", snapshot.Stages)
	}
	if snapshot.ErrorCounts["server"] != 2 {
		t.Fatalf("error counts = %#v", snapshot.ErrorCounts)
	}
	if snapshot.Throughput <= 0 || snapshot.ETA < 0 {
		t.Fatalf("throughput/ETA = %f/%s", snapshot.Throughput, snapshot.ETA)
	}
}

// TestSafetyReviewStatusWatchNonTTYAndTTY 验证 watch 输出且随上下文退出。
func TestSafetyReviewStatusWatchNonTTYAndTTY(t *testing.T) {
	for _, tty := range []bool{false, true} {
		name := "non-tty"
		if tty {
			name = "tty"
		}
		t.Run(name, func(t *testing.T) {
			store, taskID, _ := newSafetyReviewExportStore(t)
			reporter, err := NewSafetyReviewStatusReporter(SafetyReviewStatusConfig{
				TaskID: taskID, Store: store, Interval: time.Millisecond, TTY: tty,
			})
			if err != nil {
				t.Fatalf("NewSafetyReviewStatusReporter() error = %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			var output strings.Builder
			w := &cancelSafetyReviewWriter{ctx: ctx, cancel: cancel, writer: &output}
			if err := reporter.Watch(ctx, taskID, w); err != nil {
				t.Fatalf("Watch() error = %v", err)
			}
			if !strings.Contains(output.String(), "task_id="+taskID) ||
				!strings.Contains(output.String(), "status=") {
				t.Fatalf("watch output = %q", output.String())
			}
		})
	}
}

// TestSafetyReviewStatusWatchReadOnly 验证只读连接可执行 watch。
func TestSafetyReviewStatusWatchReadOnly(t *testing.T) {
	_, taskID, dbPath := newSafetyReviewExportStore(t)
	readOnly, err := dao.OpenSafetyReviewReadOnly(
		context.Background(), dbPath,
	)
	if err != nil {
		t.Fatalf("OpenSafetyReviewReadOnly() error = %v", err)
	}
	defer func() { _ = readOnly.Close() }()
	reporter, err := NewSafetyReviewStatusReporter(SafetyReviewStatusConfig{
		TaskID: taskID, Store: readOnly, Interval: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewSafetyReviewStatusReporter() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var output strings.Builder
	if err := reporter.Watch(ctx, taskID, &cancelSafetyReviewWriter{
		ctx: ctx, cancel: cancel, writer: &output,
	}); err != nil {
		t.Fatalf("Watch(read-only) error = %v", err)
	}
	if !strings.Contains(output.String(), "task_id="+taskID) {
		t.Fatalf("read-only watch output = %q", output.String())
	}
}

// cancelSafetyReviewWriter 在第一次写入后取消上下文。
type cancelSafetyReviewWriter struct {
	ctx    context.Context
	cancel context.CancelFunc
	writer *strings.Builder
}

// Write 记录输出并取消 watch。
func (w *cancelSafetyReviewWriter) Write(p []byte) (int, error) {
	w.cancel()
	return w.writer.Write(p)
}
