// Package limiter 提供 Safety Review 的角色与共享组配额。
package limiter

import (
	"container/heap"
	"context"
	"fmt"
	"sync"

	"golang.org/x/time/rate"
)

// SafetyReviewQuotaConfig 指定 Safety Review 的角色与共享组配额。
type SafetyReviewQuotaConfig struct {
	Roles  map[string]SafetyReviewRoleQuota
	Groups map[string]SafetyReviewGroupQuota
}

// SafetyReviewRoleQuota 描述单个角色的并发与速率限制。
type SafetyReviewRoleQuota struct {
	Concurrency       int
	RequestsPerMinute int
	TokensPerMinute   int
}

// SafetyReviewGroupQuota 描述共享组的并发与速率限制。
type SafetyReviewGroupQuota struct {
	Concurrency       int
	RequestsPerMinute int
	TokensPerMinute   int
}

// SafetyReviewQuota 协调角色、共享组和优先级许可。
type SafetyReviewQuota struct {
	roleSem      map[string]chan struct{}
	roleRequests map[string]*rate.Limiter
	roleTokens   map[string]*rate.Limiter
	groupSem     map[string]*safetyReviewPrioritySemaphore
	groupRequest map[string]*rate.Limiter
	groupTokens  map[string]*rate.Limiter
}

// NewSafetyReviewQuota 构造并校验 Safety Review 配额。
func NewSafetyReviewQuota(cfg SafetyReviewQuotaConfig) (*SafetyReviewQuota, error) {
	quota := &SafetyReviewQuota{
		roleSem:      make(map[string]chan struct{}, len(cfg.Roles)),
		roleRequests: make(map[string]*rate.Limiter, len(cfg.Roles)),
		roleTokens:   make(map[string]*rate.Limiter, len(cfg.Roles)),
		groupSem:     make(map[string]*safetyReviewPrioritySemaphore, len(cfg.Groups)),
		groupRequest: make(map[string]*rate.Limiter, len(cfg.Groups)),
		groupTokens:  make(map[string]*rate.Limiter, len(cfg.Groups)),
	}
	for role, limits := range cfg.Roles {
		if limits.Concurrency <= 0 {
			return nil, fmt.Errorf("safety review role %s concurrency must be positive", role)
		}
		if limits.RequestsPerMinute < 0 || limits.TokensPerMinute < 0 {
			return nil, fmt.Errorf("safety review role %s rates must not be negative", role)
		}
		quota.roleSem[role] = make(chan struct{}, limits.Concurrency)
		if limits.RequestsPerMinute > 0 {
			quota.roleRequests[role] = rate.NewLimiter(
				rate.Limit(float64(limits.RequestsPerMinute)/60),
				1,
			)
		}
		if limits.TokensPerMinute > 0 {
			quota.roleTokens[role] = rate.NewLimiter(
				rate.Limit(float64(limits.TokensPerMinute)/60),
				limits.TokensPerMinute,
			)
		}
	}
	for group, limits := range cfg.Groups {
		if limits.Concurrency <= 0 {
			return nil, fmt.Errorf("safety review group %s concurrency must be positive", group)
		}
		if limits.RequestsPerMinute < 0 || limits.TokensPerMinute < 0 {
			return nil, fmt.Errorf("safety review group %s rates must not be negative", group)
		}
		quota.groupSem[group] = newSafetyReviewPrioritySemaphore(limits.Concurrency)
		if limits.RequestsPerMinute > 0 {
			quota.groupRequest[group] = rate.NewLimiter(
				rate.Limit(float64(limits.RequestsPerMinute)/60),
				1,
			)
		}
		if limits.TokensPerMinute > 0 {
			quota.groupTokens[group] = rate.NewLimiter(
				rate.Limit(float64(limits.TokensPerMinute)/60),
				limits.TokensPerMinute,
			)
		}
	}
	return quota, nil
}

// Acquire 按角色与共享组约束获取一个幂等可释放的许可。
func (q *SafetyReviewQuota) Acquire(
	ctx context.Context,
	role string,
	group string,
	estimatedTokens int,
) (func(), error) {
	if estimatedTokens < 0 {
		return nil, fmt.Errorf("estimated tokens must not be negative")
	}
	if err := q.waitRoleRates(ctx, role, estimatedTokens); err != nil {
		return nil, err
	}
	if err := q.waitGroupRates(ctx, group, estimatedTokens); err != nil {
		return nil, err
	}

	var groupRelease func()
	if group != "" {
		if sem, ok := q.groupSem[group]; ok {
			release, err := sem.acquire(ctx, safetyReviewPriority(role))
			if err != nil {
				return nil, err
			}
			groupRelease = release
		}
	}

	var roleRelease func()
	if sem, ok := q.roleSem[role]; ok {
		select {
		case sem <- struct{}{}:
			var once sync.Once
			roleRelease = func() {
				once.Do(func() { <-sem })
			}
		case <-ctx.Done():
			if groupRelease != nil {
				groupRelease()
			}
			return nil, ctx.Err()
		}
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			if roleRelease != nil {
				roleRelease()
			}
			if groupRelease != nil {
				groupRelease()
			}
		})
	}, nil
}

// waiting 返回共享组中当前等待的请求数。
func (q *SafetyReviewQuota) waiting() int {
	total := 0
	for _, sem := range q.groupSem {
		total += sem.waiting()
	}
	return total
}

// waitRoleRates 等待角色请求与 Token 速率预算。
func (q *SafetyReviewQuota) waitRoleRates(ctx context.Context, role string, tokens int) error {
	if limiter := q.roleTokens[role]; limiter != nil && tokens > limiter.Burst() {
		return ErrTokenBudgetExceeded
	}
	if limiter := q.roleRequests[role]; limiter != nil {
		if err := limiter.Wait(ctx); err != nil {
			return fmt.Errorf("wait role request rate: %w", err)
		}
	}
	if limiter := q.roleTokens[role]; limiter != nil {
		if err := limiter.WaitN(ctx, tokens); err != nil {
			return fmt.Errorf("wait role token rate: %w", err)
		}
	}
	return nil
}

// waitGroupRates 等待共享组请求与 Token 速率预算。
func (q *SafetyReviewQuota) waitGroupRates(ctx context.Context, group string, tokens int) error {
	if group == "" {
		return nil
	}
	if limiter := q.groupTokens[group]; limiter != nil && tokens > limiter.Burst() {
		return ErrTokenBudgetExceeded
	}
	if limiter := q.groupRequest[group]; limiter != nil {
		if err := limiter.Wait(ctx); err != nil {
			return fmt.Errorf("wait group request rate: %w", err)
		}
	}
	if limiter := q.groupTokens[group]; limiter != nil {
		if err := limiter.WaitN(ctx, tokens); err != nil {
			return fmt.Errorf("wait group token rate: %w", err)
		}
	}
	return nil
}

// safetyReviewPriority 返回角色的共享组优先级。
func safetyReviewPriority(role string) int {
	switch role {
	case "expert":
		return 0
	case "router":
		return 1
	default:
		return 2
	}
}

// safetyReviewQuotaWaiter 表示一个等待共享组许可的请求。
type safetyReviewQuotaWaiter struct {
	priority int
	sequence int
	done     chan struct{}
}

// safetyReviewQuotaHeap 实现按优先级和到达顺序排序的最小堆。
type safetyReviewQuotaHeap []*safetyReviewQuotaWaiter

// Len 返回等待者数量。
func (h safetyReviewQuotaHeap) Len() int { return len(h) }

// Less 比较优先级和到达顺序。
func (h safetyReviewQuotaHeap) Less(i, j int) bool {
	if h[i].priority != h[j].priority {
		return h[i].priority < h[j].priority
	}
	return h[i].sequence < h[j].sequence
}

// Swap 交换两个等待者。
func (h safetyReviewQuotaHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

// Push 添加一个等待者。
func (h *safetyReviewQuotaHeap) Push(value any) {
	*h = append(*h, value.(*safetyReviewQuotaWaiter))
}

// Pop 移除并返回优先级最高的等待者。
func (h *safetyReviewQuotaHeap) Pop() any {
	old := *h
	item := old[len(old)-1]
	old[len(old)-1] = nil
	*h = old[:len(old)-1]
	return item
}

// safetyReviewPrioritySemaphore 实现优先级与 FIFO 的共享并发许可。
type safetyReviewPrioritySemaphore struct {
	capacity int
	inUse    int
	mu       sync.Mutex
	waiters  safetyReviewQuotaHeap
	sequence int
}

// newSafetyReviewPrioritySemaphore 构造优先级信号量。
func newSafetyReviewPrioritySemaphore(capacity int) *safetyReviewPrioritySemaphore {
	return &safetyReviewPrioritySemaphore{
		capacity: capacity,
		waiters:  make(safetyReviewQuotaHeap, 0),
	}
}

// acquire 等待优先级许可并返回幂等释放函数。
func (s *safetyReviewPrioritySemaphore) acquire(ctx context.Context, priority int) (func(), error) {
	s.mu.Lock()
	waiter := &safetyReviewQuotaWaiter{
		priority: priority,
		sequence: s.sequence,
		done:     make(chan struct{}),
	}
	s.sequence++
	heap.Push(&s.waiters, waiter)
	s.dispatchLocked()
	s.mu.Unlock()

	select {
	case <-waiter.done:
		var once sync.Once
		return func() {
			once.Do(func() {
				s.mu.Lock()
				s.inUse--
				s.dispatchLocked()
				s.mu.Unlock()
			})
		}, nil
	case <-ctx.Done():
		s.mu.Lock()
		removed := s.removeLocked(waiter)
		s.mu.Unlock()
		if removed {
			return nil, ctx.Err()
		}
		<-waiter.done
		var once sync.Once
		return func() {
			once.Do(func() {
				s.mu.Lock()
				s.inUse--
				s.dispatchLocked()
				s.mu.Unlock()
			})
		}, nil
	}
}

// waiting 返回当前排队等待者数量。
func (s *safetyReviewPrioritySemaphore) waiting() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.waiters)
}

// removeLocked 从等待队列中移除指定请求。
func (s *safetyReviewPrioritySemaphore) removeLocked(waiter *safetyReviewQuotaWaiter) bool {
	for index, current := range s.waiters {
		if current == waiter {
			heap.Remove(&s.waiters, index)
			return true
		}
	}
	return false
}

// dispatchLocked 在容量允许时唤醒优先级最高的等待者。
func (s *safetyReviewPrioritySemaphore) dispatchLocked() {
	for s.inUse < s.capacity && len(s.waiters) > 0 {
		waiter := heap.Pop(&s.waiters).(*safetyReviewQuotaWaiter)
		s.inUse++
		close(waiter.done)
	}
}
