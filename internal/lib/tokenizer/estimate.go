// Package tokenizer 提供保守且与供应商无关的 Token 估算。
package tokenizer

import (
	"unicode/utf8"

	"sendllm/internal/dto"
)

const messageOverhead = 4

// Estimate 估算消息和最大输出所需的 Token 数。
func Estimate(messages []dto.Message, maxOutputTokens int) int {
	total := maxOutputTokens
	for _, message := range messages {
		contentBytes := len(message.Content) / 4
		contentRunes := utf8.RuneCountInString(message.Content)
		contentTokens := max(contentBytes, contentRunes)
		total += messageOverhead + contentTokens
	}
	return total
}
