package dto_test

import (
	"errors"
	"testing"

	"sendllm/internal/dto"
)

func TestProviderError(t *testing.T) {
	cause := errors.New("network unavailable")
	give := &dto.ProviderError{
		Kind:       dto.ProviderRateLimited,
		StatusCode: 429,
		Err:        cause,
	}
	if !errors.Is(give, cause) {
		t.Error("ProviderError must unwrap its cause")
	}
	if got, want := give.Error(), "provider error: rate_limited (429)"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
