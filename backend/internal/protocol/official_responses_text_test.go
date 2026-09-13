package protocol

import (
	"context"
	"strings"
	"testing"
)

// Responses API 的原始 REST 响应没有 output_text —— 那是官方 SDK 合成的便利字段，
// 直接 HTTP 调用拿不到，正文在 output[].content[].text。声明式插件若只声明 output_text，
// 真实渠道会把整段回答判成空结果，任务报「已完成但没有返回结果」。
const responsesRawBody = `{
  "id": "resp_1", "object": "response", "created_at": 1, "status": "completed",
  "background": false, "completed_at": 2, "content_filters": [], "error": null,
  "frequency_penalty": 0, "incomplete_details": null,
  "output": [
    {"type": "reasoning", "id": "rs_1", "summary": []},
    {"type": "message", "id": "msg_1", "role": "assistant", "status": "completed",
     "content": [{"type": "output_text", "text": "真实回答", "annotations": []}]}
  ],
  "usage": {"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}
}`

func TestOfficialResponsesExtractsTextWithoutSDKOutputText(t *testing.T) {
	adapter := officialPackageAdapter(t, "openai-responses.yingce-plugin", "openai-response")

	created, err := adapter.ParseCreate(context.Background(), []byte(responsesRawBody))
	if err != nil {
		t.Fatal(err)
	}
	if created.Result == nil {
		t.Fatal("原始 Responses 响应被判成空结果，正文在 output[].content[].text")
	}
	if created.Result.Text != "真实回答" {
		t.Fatalf("text = %q, want 真实回答", created.Result.Text)
	}
	if created.Status != StatusSucceeded {
		t.Fatalf("status = %q, want succeeded", created.Status)
	}
}

// 有些网关会补上 SDK 形状的 output_text，此时仍要优先使用它。
func TestOfficialResponsesPrefersOutputTextWhenPresent(t *testing.T) {
	adapter := officialPackageAdapter(t, "openai-responses.yingce-plugin", "openai-response")
	body := `{"output_text":"网关摘要","output":[{"type":"message","content":[{"type":"output_text","text":"真实回答"}]}]}`
	created, err := adapter.ParseCreate(context.Background(), []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if created.Result == nil || created.Result.Text != "网关摘要" {
		t.Fatalf("result = %#v, want the SDK-shaped output_text", created.Result)
	}
}

// 思考项排在前面、或正文分成多个 part 时，都要收集齐全，不能只取固定下标。
func TestOfficialResponsesCollectsEveryMessagePart(t *testing.T) {
	adapter := officialPackageAdapter(t, "openai-responses.yingce-plugin", "openai-response")
	body := `{"output":[
      {"type":"reasoning","summary":[{"type":"summary_text","text":"思考"}]},
      {"type":"message","content":[{"type":"output_text","text":"前"},{"type":"output_text","text":"后"}]},
      {"type":"function_call","name":"canvas_get_state","arguments":"{}","call_id":"call-1"}
    ]}`
	created, err := adapter.ParseCreate(context.Background(), []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if created.Result == nil || created.Result.Text != "前后" {
		t.Fatalf("result = %#v, want 前后", created.Result)
	}
}

// 声明式 Agent 走的是 agentResponse，同一个根因会影响画布助手的纯文本回复。
func TestOfficialResponsesAgentExtractsTextWithoutSDKOutputText(t *testing.T) {
	adapter := officialPackageAdapter(t, "openai-responses.yingce-plugin", "openai-response")
	agent, ok := adapter.(AgentAdapter)
	if !ok {
		t.Fatal("openai-responses adapter does not implement AgentAdapter")
	}
	result, err := agent.ParseAgent(context.Background(), []byte(responsesRawBody))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(result.Text) != "真实回答" {
		t.Fatalf("agent text = %q, want 真实回答", result.Text)
	}
}
