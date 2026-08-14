package service

import (
	"context"
	"errors"
	"net"
	"time"

	"sendllm/internal/dto"
)

// FailureDecision 描述调用失败后的重试处理。
type FailureDecision struct {
	Category       string
	Retry          bool
	GlobalCooldown bool
	RetryAfter     time.Duration
}

// RetryPolicy 指定调用错误的指数退避参数。
type RetryPolicy struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

// Delay 返回本次重试前应等待的时间。
func (p RetryPolicy) Delay(attempt int, retryAfter time.Duration, jitter func(time.Duration) time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}

	if attempt < 1 {
		attempt = 1
	}
	limit := p.InitialBackoff
	if limit > p.MaxBackoff {
		limit = p.MaxBackoff
	}
	for remaining := attempt - 1; remaining > 0 && limit < p.MaxBackoff; remaining-- {
		limit *= 2
		if limit > p.MaxBackoff {
			limit = p.MaxBackoff
		}
	}
	return jitter(limit)
}

// ClassifyFailure 将供应商和网络错误转换为重试决策。
func ClassifyFailure(err error) FailureDecision {
	var providerErr *dto.ProviderError
	if errors.As(err, &providerErr) {
		return classifyProviderFailure(providerErr)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return FailureDecision{Category: string(dto.ProviderTimeout), Retry: true}
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) {
		return FailureDecision{Category: string(dto.ProviderNetwork), Retry: true}
	}
	return FailureDecision{Category: "unknown"}
}

func classifyProviderFailure(err *dto.ProviderError) FailureDecision {
	decision := FailureDecision{Category: string(err.Kind), RetryAfter: err.RetryAfter}
	switch err.Kind {
	case dto.ProviderNetwork, dto.ProviderTimeout, dto.ProviderServer, dto.ProviderMalformedResponse:
		decision.Retry = true
	case dto.ProviderRateLimited:
		decision.Retry = true
		decision.GlobalCooldown = true
	}
	return decision
}
