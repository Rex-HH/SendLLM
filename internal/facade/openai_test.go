package facade_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sendllm/internal/dto"
	"sendllm/internal/facade"
)

const responseSchema = `{"type":"object","properties":{"label":{"type":"string"}}}`
const responseContent = `{"label":"safe","explanation":"内容没有攻击或规避安全控制的意图"}`

func TestOpenAI_CompleteSendsConfiguredResponseFormat(t *testing.T) {
	tests := []struct {
		name       string
		mode       string
		wantFormat map[string]any
	}{
		{
			name: "json schema",
			mode: "json_schema",
			wantFormat: map[string]any{
				"type": "json_schema",
				"json_schema": map[string]any{
					"name":   "safety_annotation",
					"strict": true,
					"schema": map[string]any{"type": "object", "properties": map[string]any{"label": map[string]any{"type": "string"}}},
				},
			},
		},
		{name: "json object", mode: "json_object", wantFormat: map[string]any{"type": "json_object"}},
		{name: "prompt only", mode: "prompt_only"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %s, want POST", r.Method)
				}
				if r.URL.Path != "/chat/completions" {
					t.Errorf("path = %s, want /chat/completions", r.URL.Path)
				}
				if got, want := r.Header.Get("Authorization"), "Bearer test-key"; got != want {
					t.Errorf("Authorization = %q, want %q", got, want)
				}
				if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
					t.Errorf("Content-Type = %q, want application/json", got)
				}
				var give map[string]any
				if err := json.NewDecoder(r.Body).Decode(&give); err != nil {
					t.Errorf("decode request: %v", err)
				}
				if got, want := give["model"], "test-model"; got != want {
					t.Errorf("model = %v, want %v", got, want)
				}
				if test.wantFormat == nil {
					if _, ok := give["response_format"]; ok {
						t.Error("prompt_only request included response_format")
					}
				} else if got, ok := give["response_format"].(map[string]any); !ok || !equalJSON(got, test.wantFormat) {
					t.Errorf("response_format = %#v, want %#v", give["response_format"], test.wantFormat)
				}
				_, _ = fmt.Fprint(w, completionResponse("stop"))
			}))
			t.Cleanup(server.Close)

			client := newTestClient(t, server.URL)
			got, err := client.Complete(context.Background(), completionRequest(test.mode))
			if err != nil {
				t.Fatalf("Complete() error = %v", err)
			}
			if got.FinishReason != "stop" {
				t.Errorf("FinishReason = %q, want stop", got.FinishReason)
			}
			if string(got.Content) != responseContent {
				t.Errorf("Content = %q, want structured response", got.Content)
			}
			if got.Usage.TotalTokens != 12 {
				t.Errorf("Usage.TotalTokens = %d, want 12", got.Usage.TotalTokens)
			}
		})
	}
}

func TestOpenAI_CompleteRateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "2")
		http.Error(w, `{"error":{"message":"rate limited"}}`, http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)
	_, err := client.Complete(context.Background(), completionRequest("json_schema"))
	var providerErr *dto.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != dto.ProviderRateLimited {
		t.Fatalf("Complete() error = %v, want rate-limited ProviderError", err)
	}
	if providerErr.RetryAfter != 2*time.Second {
		t.Errorf("RetryAfter = %v, want 2s", providerErr.RetryAfter)
	}
}

func TestOpenAI_CompleteSendsSamplingParametersAndExtraBody(t *testing.T) {
	temperature := 0.2
	topP := 0.8
	seed := int64(42)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var give map[string]any
		if err := json.NewDecoder(r.Body).Decode(&give); err != nil {
			t.Errorf("decode request: %v", err)
		}
		for key, want := range map[string]any{
			"temperature":       temperature,
			"top_p":             topP,
			"max_tokens":        float64(100),
			"seed":              float64(seed),
			"frequency_penalty": float64(0),
		} {
			if got := give[key]; got != want {
				t.Errorf("%s = %v, want %v", key, got, want)
			}
		}
		_, _ = fmt.Fprint(w, completionResponse("stop"))
	}))
	t.Cleanup(server.Close)

	client, err := facade.NewOpenAI(facade.Config{
		BaseURL:        server.URL,
		APIKey:         "test-key",
		Model:          "test-model",
		Temperature:    &temperature,
		TopP:           &topP,
		MaxTokens:      100,
		Seed:           &seed,
		ExtraBody:      map[string]any{"frequency_penalty": 0},
		Timeout:        time.Second,
		MaxConnections: 2,
	})
	if err != nil {
		t.Fatalf("NewOpenAI() error = %v", err)
	}
	if _, err := client.Complete(context.Background(), completionRequest("json_schema")); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
}

func TestOpenAI_CompleteParsesHTTPDateRetryAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", time.Now().Add(3*time.Second).UTC().Format(http.TimeFormat))
		http.Error(w, `{"error":{"message":"rate limited"}}`, http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)
	_, err := client.Complete(context.Background(), completionRequest("json_schema"))
	var providerErr *dto.ProviderError
	if !errors.As(err, &providerErr) || providerErr.RetryAfter <= 0 || providerErr.RetryAfter > 3*time.Second {
		t.Fatalf("Complete() error = %v, want positive Retry-After no greater than 3s", err)
	}
}

func TestOpenAI_CompleteClassifiesProviderFailures(t *testing.T) {
	tests := []struct {
		name string
		code int
		kind dto.ProviderErrorKind
	}{
		{name: "authentication", code: http.StatusUnauthorized, kind: dto.ProviderAuthentication},
		{name: "server", code: http.StatusInternalServerError, kind: dto.ProviderServer},
		{name: "bad request", code: http.StatusBadRequest, kind: dto.ProviderBadRequest},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, `{"error":{"message":"request failed"}}`, test.code)
			}))
			t.Cleanup(server.Close)

			client := newTestClient(t, server.URL)
			_, err := client.Complete(context.Background(), completionRequest("json_schema"))
			var providerErr *dto.ProviderError
			if !errors.As(err, &providerErr) || providerErr.Kind != test.kind {
				t.Fatalf("Complete() error = %v, want %s ProviderError", err, test.kind)
			}
		})
	}
}

func TestOpenAI_CompleteRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, strings.Repeat("x", 4*1024*1024+1))
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)
	_, err := client.Complete(context.Background(), completionRequest("json_schema"))
	var providerErr *dto.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != dto.ProviderMalformedResponse {
		t.Fatalf("Complete() error = %v, want malformed response ProviderError", err)
	}
}

func TestOpenAI_CompleteClassifiesContentFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, completionResponse("content_filter"))
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)
	_, err := client.Complete(context.Background(), completionRequest("json_schema"))
	var providerErr *dto.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != dto.ProviderContentRejected {
		t.Fatalf("Complete() error = %v, want content rejected ProviderError", err)
	}
}

func newTestClient(t *testing.T, baseURL string) *facade.OpenAI {
	t.Helper()
	client, err := facade.NewOpenAI(facade.Config{
		BaseURL:        baseURL,
		APIKey:         "test-key",
		Model:          "test-model",
		Timeout:        time.Second,
		MaxConnections: 2,
	})
	if err != nil {
		t.Fatalf("NewOpenAI() error = %v", err)
	}
	return client
}

func completionRequest(mode string) dto.CompletionRequest {
	return dto.CompletionRequest{
		Messages: []dto.Message{{Role: "user", Content: "synthetic request"}},
		Schema:   json.RawMessage(responseSchema),
		Mode:     mode,
	}
}

func completionResponse(finishReason string) string {
	return `{"choices":[{"message":{"role":"assistant","content":` + fmt.Sprintf("%q", responseContent) + `},"finish_reason":` + fmt.Sprintf("%q", finishReason) + `}],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`
}

func equalJSON(left, right any) bool {
	leftBytes, err := json.Marshal(left)
	if err != nil {
		return false
	}
	rightBytes, err := json.Marshal(right)
	if err != nil {
		return false
	}
	return string(leftBytes) == string(rightBytes)
}
