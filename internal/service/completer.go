package service

import (
	"context"

	"sendllm/internal/dto"
)

// Completer 定义模型补全所需的最小边界。
type Completer interface {
	Complete(ctx context.Context, req dto.CompletionRequest) (dto.CompletionResponse, error)
}
