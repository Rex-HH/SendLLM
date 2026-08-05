package service

import (
	"time"

	"sendllm/internal/dao"
)

// Summary 是不包含数据载荷的任务进度快照。
type Summary struct {
	Pending   int64
	Retrying  int64
	Succeeded int64
	Failed    int64
	Rate      float64
	ETA       time.Duration
}

type progressTracker struct {
	startedAt   time.Time
	initialDone int64
}

func newProgressTracker(now time.Time, counts dao.Counts) progressTracker {
	return progressTracker{
		startedAt:   now,
		initialDone: counts.Succeeded + counts.Failed,
	}
}

func (p progressTracker) summary(now time.Time, counts dao.Counts) Summary {
	elapsed := now.Sub(p.startedAt).Seconds()
	completed := counts.Succeeded + counts.Failed - p.initialDone
	var rate float64
	if elapsed > 0 && completed > 0 {
		rate = float64(completed) / elapsed
	}
	remaining := counts.Pending + counts.Processing + counts.RetryWait
	var eta time.Duration
	if rate > 0 && remaining > 0 {
		eta = time.Duration(float64(remaining) / rate * float64(time.Second))
	}
	return Summary{
		Pending:   counts.Pending + counts.Processing,
		Retrying:  counts.RetryWait,
		Succeeded: counts.Succeeded,
		Failed:    counts.Failed,
		Rate:      rate,
		ETA:       eta,
	}
}
