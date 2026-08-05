package tokenizer_test

import (
	"testing"

	"sendllm/internal/dto"
	"sendllm/internal/lib/tokenizer"
)

func TestEstimate(t *testing.T) {
	tests := []struct {
		name            string
		messages        []dto.Message
		maxOutputTokens int
		want            int
	}{
		{
			name: "ASCII message",
			messages: []dto.Message{{
				Role:    "user",
				Content: "hello",
			}},
			maxOutputTokens: 10,
			want:            19,
		},
		{
			name: "Chinese message",
			messages: []dto.Message{{
				Role:    "user",
				Content: "你好",
			}},
			maxOutputTokens: 10,
			want:            16,
		},
		{
			name: "empty message",
			messages: []dto.Message{{
				Role: "user",
			}},
			maxOutputTokens: 10,
			want:            14,
		},
		{
			name: "multiple messages",
			messages: []dto.Message{
				{Role: "system", Content: "a"},
				{Role: "user", Content: "测试"},
			},
			maxOutputTokens: 7,
			want:            18,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := tokenizer.Estimate(test.messages, test.maxOutputTokens)
			if got != test.want {
				t.Errorf("Estimate() = %d, want %d", got, test.want)
			}
		})
	}
}
