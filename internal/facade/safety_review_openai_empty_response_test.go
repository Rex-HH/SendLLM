package facade_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"sendllm/internal/dto"
)

// TestSafetyReviewOpenAIEmptySuccessBody 验证 200 空响应不会被当成成功结果。
func TestSafetyReviewOpenAIEmptySuccessBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)
	got, err := client.Complete(context.Background(), completionRequest("json_object"))
	var providerErr *dto.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != dto.ProviderMalformedResponse {
		t.Fatalf("Complete() error = %v, want malformed response ProviderError", err)
	}
	if len(got.RawResponse) != 0 {
		t.Fatalf("RawResponse length = %d, want 0", len(got.RawResponse))
	}
}
