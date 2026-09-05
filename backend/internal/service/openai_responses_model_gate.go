package service

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
)

// openAIResponsesModelChatOnlyTTL 是「模型不支持 Responses API」学习结果的进程内
// 记忆时长。同一 (account, model) 在 TTL 内的后续请求直接走 Chat Completions 直转，
// 不再白挨一次上游 400。
const openAIResponsesModelChatOnlyTTL = 10 * time.Minute

type openAIResponsesModelChatOnlyEntry struct {
	expiresAt time.Time
}

var (
	openAIResponsesChatOnlyMu     sync.RWMutex
	openAIResponsesChatOnlyModels = make(map[int64]map[string]openAIResponsesModelChatOnlyEntry)
)

// isOpenAIResponsesModelChatOnly 判断 (accountID, model) 是否已被学习为
// 「上游不支持该模型的 Responses API」。
func isOpenAIResponsesModelChatOnly(accountID int64, model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if accountID <= 0 || model == "" {
		return false
	}
	openAIResponsesChatOnlyMu.RLock()
	defer openAIResponsesChatOnlyMu.RUnlock()
	byModel, ok := openAIResponsesChatOnlyModels[accountID]
	if !ok {
		return false
	}
	entry, ok := byModel[model]
	if !ok {
		return false
	}
	return time.Now().Before(entry.expiresAt)
}

// markOpenAIResponsesModelChatOnly 记录 (accountID, model) 在上游不支持 Responses API。
// 幂等：重复触发只刷新过期时间。
func markOpenAIResponsesModelChatOnly(accountID int64, model string) {
	model = strings.ToLower(strings.TrimSpace(model))
	if accountID <= 0 || model == "" {
		return
	}
	openAIResponsesChatOnlyMu.Lock()
	defer openAIResponsesChatOnlyMu.Unlock()
	byModel, ok := openAIResponsesChatOnlyModels[accountID]
	if !ok {
		byModel = make(map[string]openAIResponsesModelChatOnlyEntry)
		openAIResponsesChatOnlyModels[accountID] = byModel
	}
	// 重复标记仅刷新过期时间。
	byModel[model] = openAIResponsesModelChatOnlyEntry{expiresAt: time.Now().Add(openAIResponsesModelChatOnlyTTL)}
}

// openAIResponsesChatOnlyExplicit 读取账号 extra 中管理员手动钉死的
// 「必须走 Chat Completions 直转」模型清单（openai_responses_chat_only_models，
// 数组或逗号分隔字符串均可）。
func openAIResponsesChatOnlyExplicit(account *Account, model string) bool {
	if account == nil || len(account.Extra) == 0 || strings.TrimSpace(model) == "" {
		return false
	}
	raw, ok := account.Extra[openai_compat.ExtraKeyResponsesChatOnlyModels]
	if !ok || raw == nil {
		return false
	}
	model = strings.ToLower(strings.TrimSpace(model))
	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && strings.ToLower(strings.TrimSpace(s)) == model {
				return true
			}
		}
	case []string:
		for _, s := range v {
			if strings.ToLower(strings.TrimSpace(s)) == model {
				return true
			}
		}
	case string:
		for _, part := range strings.Split(v, ",") {
			if strings.ToLower(strings.TrimSpace(part)) == model {
				return true
			}
		}
	}
	return false
}

// shouldForwardOpenAIResponsesViaRawChatCompletionsForModel 判断本次 /v1/responses
// （或经 Responses 中转的 /v1/messages）请求是否应对该账号走 Chat Completions 直转。
//
// 与账号级 shouldForwardOpenAIResponsesViaRawChatCompletions 相比，增加了两个
// 模型级信号：
//  1. 管理员在账号 extra.openai_responses_chat_only_models 中钉死的模型清单；
//  2. 进程内学习结果：该模型此前在上游被「当前模型不支持 Responses API」拒绝过
//     （openAIResponsesModelChatOnlyTTL 内有效）。
//
// 背景：上游（如 tokenrhythm.studio 等中转站）的 /v1/responses 支持是模型级的——
// 账号探测用某个模型通过后，其他模型（如 glm-5.3-flash）仍可能只支持 Chat
// Completions，直接打原生 Responses 会被 400 拒绝。
func shouldForwardOpenAIResponsesViaRawChatCompletionsForModel(account *Account, model string) bool {
	if shouldForwardOpenAIResponsesViaRawChatCompletions(account) {
		return true
	}
	if account == nil || account.Type != AccountTypeAPIKey {
		return false
	}
	if openAIResponsesChatOnlyExplicit(account, model) {
		return true
	}
	return isOpenAIResponsesModelChatOnly(account.ID, model)
}

// isOpenAIResponsesNotSupportedUpstreamError 识别上游「模型不支持 Responses API」
// 的确定性拒绝。匹配模式（大小写不敏感）：
//   - "当前模型不支持 responses api"（中文中转站常见文案）
//   - "model ... does not support the responses api" / "not support ... responses"
// 仅在 400/404/405 状态码下生效，避免把 5xx 误判成能力缺失。
func isOpenAIResponsesNotSupportedUpstreamError(statusCode int, upstreamMsg string, body []byte) bool {
	switch statusCode {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed:
	default:
		return false
	}
	haystack := strings.ToLower(strings.TrimSpace(upstreamMsg))
	if haystack == "" && len(body) > 0 {
		haystack = strings.ToLower(string(body))
	}
	if haystack == "" || !strings.Contains(haystack, "responses") {
		return false
	}
	return strings.Contains(haystack, "不支持") ||
		strings.Contains(haystack, "not support") ||
		strings.Contains(haystack, "does not support") ||
		strings.Contains(haystack, "doesn't support")
}
