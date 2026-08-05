package dao

import "time"

// ItemState 表示样本在持久化处理流程中的状态。
type ItemState string

const (
	// ItemPending 表示样本尚未领取。
	ItemPending ItemState = "pending"
	// ItemProcessing 表示样本已被处理器领取。
	ItemProcessing ItemState = "processing"
	// ItemRetryWait 表示样本正在等待下次重试。
	ItemRetryWait ItemState = "retry_wait"
	// ItemSucceeded 表示样本已成功完成。
	ItemSucceeded ItemState = "succeeded"
	// ItemFailed 表示样本已最终失败。
	ItemFailed ItemState = "failed"
)

// Item 表示一条待处理或已处理的源样本状态。
type Item struct {
	TaskID          string
	TraceID         string
	InputIndex      int64
	SourceHash      string
	RawJSON         []byte
	Prompt          string
	Response        string
	State           ItemState
	RequestAttempts int
	RepairAttempts  int
	NextAttemptAt   time.Time
}

// ImportDisposition 表示一条样本在导入中的处理结果。
type ImportDisposition int

const (
	// ImportAdded 表示样本已写入当前导入事务。
	ImportAdded ImportDisposition = iota + 1
	// ImportSkipped 表示内容相同的样本已经存在。
	ImportSkipped
)
