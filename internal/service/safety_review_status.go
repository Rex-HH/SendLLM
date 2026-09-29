// Package service 提供 Safety Review 的只读状态摘要。
package service

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"sendllm/internal/dao"
)

// SafetyReviewStatusStore 是状态摘要需要的只读持久化边界。
type SafetyReviewStatusStore interface {
	ReadSafetyReviewDetailedSummary(ctx context.Context, taskID string) (dao.SafetyReviewDetailedSummary, error)
}

// SafetyReviewStatusConfig 指定状态读取与 watch 行为。
type SafetyReviewStatusConfig struct {
	TaskID   string
	Store    SafetyReviewStatusStore
	Interval time.Duration
	TTY      bool
	Now      func() time.Time
}

// SafetyReviewStatusSnapshot 表示一次只读状态摘要。
type SafetyReviewStatusSnapshot struct {
	TaskID      string
	Status      string
	Scene       string
	Items       map[string]int64
	Stages      map[string]int64
	Decisions   int64
	Throughput  float64
	ETA         time.Duration
	ErrorCounts map[string]int64
}

// SafetyReviewStatusReporter 生成 Safety Review 状态摘要。
type SafetyReviewStatusReporter struct {
	cfg SafetyReviewStatusConfig
}

// NewSafetyReviewStatusReporter 校验依赖并构造状态报告器。
func NewSafetyReviewStatusReporter(cfg SafetyReviewStatusConfig) (*SafetyReviewStatusReporter, error) {
	if cfg.TaskID == "" || cfg.Store == nil {
		return nil, fmt.Errorf("safety review status dependencies are incomplete")
	}
	if cfg.Interval <= 0 {
		return nil, fmt.Errorf("safety review status interval must be positive")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &SafetyReviewStatusReporter{cfg: cfg}, nil
}

// Snapshot 读取并计算一次状态摘要。
func (r *SafetyReviewStatusReporter) Snapshot(
	ctx context.Context,
	taskID string,
) (SafetyReviewStatusSnapshot, error) {
	if taskID != r.cfg.TaskID {
		return SafetyReviewStatusSnapshot{}, fmt.Errorf("safety review status task mismatch")
	}
	summary, err := r.cfg.Store.ReadSafetyReviewDetailedSummary(ctx, taskID)
	if err != nil {
		return SafetyReviewStatusSnapshot{}, fmt.Errorf("read safety review status: %w", err)
	}
	total := int64(0)
	terminal := int64(0)
	for state, count := range summary.Items {
		total += count
		if state == "resolved_safe" || state == "resolved_unsafe" || state == "quarantined" {
			terminal += count
		}
	}
	elapsed := r.cfg.Now().Sub(summary.CreatedAt)
	if elapsed <= 0 {
		elapsed = time.Nanosecond
	}
	throughput := float64(terminal) / elapsed.Seconds()
	var eta time.Duration
	if throughput > 0 {
		eta = time.Duration(float64(total-terminal) / throughput * float64(time.Second))
	}
	return SafetyReviewStatusSnapshot{
		TaskID: summary.TaskID, Status: summary.TaskStatus, Scene: summary.Scene,
		Items: summary.Items, Stages: summary.Stages, Decisions: summary.Decisions,
		Throughput: throughput, ETA: eta, ErrorCounts: summary.ErrorCounts,
	}, nil
}

// Watch 周期输出状态摘要，直到上下文取消。
func (r *SafetyReviewStatusReporter) Watch(ctx context.Context, taskID string, w io.Writer) error {
	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()
	for {
		snapshot, err := r.Snapshot(ctx, taskID)
		if err != nil {
			return err
		}
		if r.cfg.TTY {
			_, err = fmt.Fprintf(w, "\r%s", formatSafetyReviewStatus(snapshot))
		} else {
			_, err = fmt.Fprintln(w, formatSafetyReviewStatus(snapshot))
		}
		if err != nil {
			return fmt.Errorf("write safety review status: %w", err)
		}
		if ctx.Err() != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// formatSafetyReviewStatus 输出紧凑且不含载荷的状态行。
func formatSafetyReviewStatus(snapshot SafetyReviewStatusSnapshot) string {
	itemsTotal := int64(0)
	itemsTerminal := int64(0)
	for state, count := range snapshot.Items {
		itemsTotal += count
		if state == "resolved_safe" || state == "resolved_unsafe" || state == "quarantined" {
			itemsTerminal += count
		}
	}
	stagesTotal := int64(0)
	for _, count := range snapshot.Stages {
		stagesTotal += count
	}
	errors := make([]string, 0, len(snapshot.ErrorCounts))
	for category := range snapshot.ErrorCounts {
		errors = append(errors, category)
	}
	sort.Strings(errors)
	errorParts := make([]string, 0, len(errors))
	for _, category := range errors {
		errorParts = append(errorParts, fmt.Sprintf("%s:%d", category, snapshot.ErrorCounts[category]))
	}
	return fmt.Sprintf(
		"task_id=%s status=%s scene=%s items_total=%d items_terminal=%d "+
			"stages_total=%d decisions=%d throughput=%.6f eta=%s errors=%s",
		snapshot.TaskID, snapshot.Status, snapshot.Scene, itemsTotal, itemsTerminal,
		stagesTotal, snapshot.Decisions, snapshot.Throughput, snapshot.ETA,
		strings.Join(errorParts, ","),
	)
}

// FormatSafetyReviewStatus 输出安全状态摘要行。
func FormatSafetyReviewStatus(snapshot SafetyReviewStatusSnapshot) string {
	return formatSafetyReviewStatus(snapshot)
}
