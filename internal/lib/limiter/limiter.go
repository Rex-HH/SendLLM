// Package limiter 提供并发、请求和 Token 的本地限速。
package limiter

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

var (
	// ErrTokenBudgetExceeded 表示单次估算 Token 超过整分钟预算。
	ErrTokenBudgetExceeded = errors.New("token budget exceeded")
	// ErrInvalidConfig 表示限速器配置无法执行。
	ErrInvalidConfig = errors.New("invalid limiter configuration")
)

// Config 指定并发、请求和 Token 限额。
type Config struct {
	Concurrency       int
	RequestsPerMinute int
	TokensPerMinute   int
}

// Limiter 协调并发许可、速率令牌和共享冷却。
type Limiter struct {
	// semaphore 的容量等于并发配置，用于对请求方施加有界背压。
	semaphore chan struct{}
	requests  *rate.Limiter
	tokens    *rate.Limiter

	// mu 保护 coolUntil，临界区只读取或延长共享冷却时间。
	mu        sync.Mutex
	coolUntil time.Time
}

// New 创建已校验的限速器。
func New(cfg Config) (*Limiter, error) {
	if cfg.Concurrency < 1 {
		return nil, fmt.Errorf("%w: concurrency must be positive", ErrInvalidConfig)
	}
	if cfg.RequestsPerMinute < 0 || cfg.TokensPerMinute < 0 {
		return nil, fmt.Errorf("%w: rates must not be negative", ErrInvalidConfig)
	}

	value := &Limiter{semaphore: make(chan struct{}, cfg.Concurrency)}
	if cfg.RequestsPerMinute > 0 {
		value.requests = rate.NewLimiter(rate.Limit(float64(cfg.RequestsPerMinute)/60), 1)
	}
	if cfg.TokensPerMinute > 0 {
		value.tokens = rate.NewLimiter(rate.Limit(float64(cfg.TokensPerMinute)/60), cfg.TokensPerMinute)
	}
	return value, nil
}

// Acquire 等待冷却、请求和 Token 配额后取得并发许可。
func (l *Limiter) Acquire(ctx context.Context, estimatedTokens int) (func(), error) {
	if l.tokens != nil && estimatedTokens > l.tokens.Burst() {
		return nil, ErrTokenBudgetExceeded
	}
	if err := l.waitCooldown(ctx); err != nil {
		return nil, err
	}
	if l.requests != nil {
		if err := l.requests.Wait(ctx); err != nil {
			return nil, fmt.Errorf("wait request rate: %w", err)
		}
	}
	if l.tokens != nil {
		if err := l.tokens.WaitN(ctx, estimatedTokens); err != nil {
			return nil, fmt.Errorf("wait token rate: %w", err)
		}
	}
	if err := l.waitCooldown(ctx); err != nil {
		return nil, err
	}
	select {
	case l.semaphore <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			<-l.semaphore
		})
	}, nil
}

// Cooldown 延长全部调用方共享的冷却截止时间。
func (l *Limiter) Cooldown(until time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if until.After(l.coolUntil) {
		l.coolUntil = until
	}
}

func (l *Limiter) waitCooldown(ctx context.Context) error {
	for {
		l.mu.Lock()
		until := l.coolUntil
		l.mu.Unlock()

		wait := time.Until(until)
		if wait <= 0 {
			return nil
		}

		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		}
	}
}
