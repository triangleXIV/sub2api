package service

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesModelGate_LearnedModelShortCircuitsChatCompletions(t *testing.T) {
	account := &Account{ID: 77, Type: AccountTypeAPIKey}
	require.False(t, shouldForwardOpenAIResponsesViaRawChatCompletionsForModel(account, "glm-5.3-flash"))

	markOpenAIResponsesModelChatOnly(account.ID, "glm-5.3-flash")
	require.True(t, shouldForwardOpenAIResponsesViaRawChatCompletionsForModel(account, "glm-5.3-flash"))
	require.True(t, shouldForwardOpenAIResponsesViaRawChatCompletionsForModel(account, "GLM-5.3-FLASH"), "模型名匹配应大小写不敏感")
	require.False(t, shouldForwardOpenAIResponsesViaRawChatCompletionsForModel(account, "deepseek-v4-flash-0731"), "其他模型不受影响")
}

func TestOpenAIResponsesModelGate_ExplicitChatOnlyModels(t *testing.T) {
	account := &Account{
		ID:   78,
		Type: AccountTypeAPIKey,
		Extra: map[string]any{
			openai_compat.ExtraKeyResponsesChatOnlyModels: []any{"glm-5.3-flash", "glm-5"},
		},
	}
	require.True(t, shouldForwardOpenAIResponsesViaRawChatCompletionsForModel(account, "glm-5.3-flash"))
	require.True(t, shouldForwardOpenAIResponsesViaRawChatCompletionsForModel(account, "glm-5"))
	require.False(t, shouldForwardOpenAIResponsesViaRawChatCompletionsForModel(account, "glm-5.2"))

	commaAccount := &Account{
		ID:   79,
		Type: AccountTypeAPIKey,
		Extra: map[string]any{
			openai_compat.ExtraKeyResponsesChatOnlyModels: "glm-5.3-flash, minimax-m2.7",
		},
	}
	require.True(t, shouldForwardOpenAIResponsesViaRawChatCompletionsForModel(commaAccount, "minimax-m2.7"))
	require.False(t, shouldForwardOpenAIResponsesViaRawChatCompletionsForModel(commaAccount, "glm-5.2"))
}

func TestOpenAIResponsesNotSupportedUpstreamError(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		message    string
		want       bool
	}{
		{"cn message", http.StatusBadRequest, "当前模型不支持 Responses API：glm-5.3-flash", true},
		{"cn message uppercase", http.StatusBadRequest, "当前模型不支持 responses API", true},
		{"en does not support", http.StatusBadRequest, "model glm-5.3-flash does not support the responses api", true},
		{"en not support", http.StatusNotFound, "not support responses", true},
		{"unrelated 400", http.StatusBadRequest, "invalid_request_error: tools too large", false},
		{"5xx never matches", http.StatusBadGateway, "当前模型不支持 Responses API", false},
		{"message without responses", http.StatusBadRequest, "当前模型不支持", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isOpenAIResponsesNotSupportedUpstreamError(tc.statusCode, tc.message, nil))
		})
	}
}
