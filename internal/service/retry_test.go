package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"sendllm/internal/dto"
)

func TestRetryPolicyDelay(t *testing.T) {
	policy := RetryPolicy{
		MaxAttempts:    5,
		InitialBackoff: time.Second,
		MaxBackoff:     4 * time.Second,
	}

	tests := []struct {
		name       string
		attempt    int
		retryAfter time.Duration
		want       time.Duration
	}{
		{name: "first attempt", attempt: 1, want: 500 * time.Millisecond},
		{name: "second attempt", attempt: 2, want: time.Second},
		{name: "backoff capped", attempt: 4, want: 2 * time.Second},
		{name: "retry after takes precedence", attempt: 4, retryAfter: 7 * time.Second, want: 7 * time.Second},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := policy.Delay(test.attempt, test.retryAfter, func(limit time.Duration) time.Duration {
				return limit / 2
			})
			if got != test.want {
				t.Errorf("Delay() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRetryPolicyDelayCapsInitialBackoff(t *testing.T) {
	policy := RetryPolicy{
		InitialBackoff: 5 * time.Second,
		MaxBackoff:     4 * time.Second,
	}

	got := policy.Delay(1, 0, func(limit time.Duration) time.Duration {
		return limit
	})
	if want := 4 * time.Second; got != want {
		t.Errorf("Delay() = %v, want %v", got, want)
	}
}

func TestClassifyFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want FailureDecision
	}{
		{
			name: "network error",
			err:  &net.DNSError{IsTemporary: true},
			want: FailureDecision{Category: "network", Retry: true},
		},
		{
			name: "context deadline",
			err:  context.DeadlineExceeded,
			want: FailureDecision{Category: "timeout", Retry: true},
		},
		{
			name: "request timeout",
			err: &dto.ProviderError{
				Kind:       dto.ProviderTimeout,
				StatusCode: http.StatusRequestTimeout,
			},
			want: FailureDecision{Category: "timeout", Retry: true},
		},
		{
			name: "rate limited",
			err: &dto.ProviderError{
				Kind:       dto.ProviderRateLimited,
				StatusCode: http.StatusTooManyRequests,
				RetryAfter: 3 * time.Second,
			},
			want: FailureDecision{
				Category:       "rate_limited",
				Retry:          true,
				GlobalCooldown: true,
				RetryAfter:     3 * time.Second,
			},
		},
		{
			name: "server error",
			err: &dto.ProviderError{
				Kind:       dto.ProviderServer,
				StatusCode: http.StatusInternalServerError,
			},
			want: FailureDecision{Category: "server", Retry: true},
		},
		{
			name: "malformed provider response",
			err:  &dto.ProviderError{Kind: dto.ProviderMalformedResponse},
			want: FailureDecision{Category: "malformed_response", Retry: true},
		},
		{
			name: "authentication error",
			err: &dto.ProviderError{
				Kind:       dto.ProviderAuthentication,
				StatusCode: http.StatusUnauthorized,
			},
			want: FailureDecision{Category: "authentication"},
		},
		{
			name: "bad request",
			err: &dto.ProviderError{
				Kind:       dto.ProviderBadRequest,
				StatusCode: http.StatusBadRequest,
			},
			want: FailureDecision{Category: "bad_request"},
		},
		{
			name: "content rejected",
			err:  &dto.ProviderError{Kind: dto.ProviderContentRejected},
			want: FailureDecision{Category: "content_rejected"},
		},
		{
			name: "unknown error",
			err:  errors.New("unknown"),
			want: FailureDecision{Category: "unknown"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ClassifyFailure(test.err)
			if got != test.want {
				t.Errorf("ClassifyFailure() = %#v, want %#v", got, test.want)
			}
		})
	}
}
