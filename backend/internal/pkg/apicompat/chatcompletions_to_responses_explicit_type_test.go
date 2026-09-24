package apicompat

import (
	"encoding/json"
	"testing"
)

// CN 上游（火山引擎 Ark 等）的 Responses 实现要求 input 数组每项都带显式
// type 字段，缺省会以 "missing `input.type` parameter" 400 拒绝。这里锁定
// chat→responses 转换对 system/user/assistant 消息的显式 type:"message"。
func TestChatCompletionsToResponses_ExplicitMessageType(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model: "deepseek-flash",
		Messages: []ChatMessage{
			{Role: "system", Content: json.RawMessage(`"be brief"`)},
			{Role: "user", Content: json.RawMessage(`"hi"`)},
			{Role: "assistant", Content: json.RawMessage(`"hello"`)},
			{Role: "user", Content: json.RawMessage(`"do it"`)},
		},
	}
	out, err := ChatCompletionsToResponses(req)
	if err != nil {
		t.Fatalf("ChatCompletionsToResponses: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(out.Input, &items); err != nil {
		t.Fatalf("unmarshal input: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("input items = %d, want 4", len(items))
	}
	wantRoles := []string{"system", "user", "assistant", "user"}
	for i, wantRole := range wantRoles {
		if got := items[i]["type"]; got != "message" {
			t.Fatalf("input[%d].type = %v, want \"message\"", i, got)
		}
		if got := items[i]["role"]; got != wantRole {
			t.Fatalf("input[%d].role = %v, want %q", i, got, wantRole)
		}
	}
}

// tool_calls 项保持 function_call 类型；纯 assistant 文本回放项带显式
// type:"message"，供多轮重放兼容严格上游。
func TestChatCompletionsToResponses_AssistantToolCallItems(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model: "deepseek-flash",
		Messages: []ChatMessage{
			{Role: "assistant", Content: nil, ToolCalls: []ChatToolCall{{
				ID:       "call_1",
				Function: ChatFunctionCall{Name: "get_weather", Arguments: `{"city":"x"}`},
			}}},
		},
	}
	out, err := ChatCompletionsToResponses(req)
	if err != nil {
		t.Fatalf("ChatCompletionsToResponses: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(out.Input, &items); err != nil {
		t.Fatalf("unmarshal input: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("input items = %d, want 1", len(items))
	}
	if got := items[0]["type"]; got != "function_call" {
		t.Fatalf("input[0].type = %v, want \"function_call\"", got)
	}
	if got := items[0]["call_id"]; got != "call_1" {
		t.Fatalf("input[0].call_id = %v, want call_1", got)
	}
}
