package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/protocol"
)

// shippedChatCompletionsAdapter 加载随仓库发布的官方 chat-completion 插件包，
// 保证断言的是真实投产协议模板，而不是内置兜底实现。
func shippedChatCompletionsAdapter(t *testing.T) protocol.Adapter {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "plugin-packages", "openai-chat-completions.yingce-plugin"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := protocol.ParsePluginPackage(data)
	if err != nil {
		t.Fatal(err)
	}
	providers, err := protocol.LoadInstalledProviders(pkg.ManifestRaw, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range providers {
		if provider.Metadata().ID == "chat-completion" {
			return provider
		}
	}
	t.Fatal("openai-chat-completions plugin does not contribute chat-completion")
	return nil
}

// 上游确实返回了正文、但不在插件声明的响应路径上时，旧行为把分片正文判成空结果，
// 报「声明式协议已完成但没有返回结果」，只能靠翻 api_call_logs.response_body 反推。
// 这里锁定两条要求：同协议内的分片正文必须能取到，取不到时必须说清是形状没命中。
func TestProtocolResultExtractionAndDiagnostics(t *testing.T) {
	adapter := shippedChatCompletionsAdapter(t)
	input := canvasGenerationInput{Mode: "text", Config: providerConfig{InterfaceType: "chat-completion"}}

	finish := func(t *testing.T, body string) (map[string]interface{}, error) {
		t.Helper()
		created, err := adapter.ParseCreate(context.Background(), []byte(body))
		if err != nil {
			t.Fatal(err)
		}
		return finishProtocolAdapterResult(context.Background(), input, adapter, protocol.GenerationRequest{}, "task-1", created.Result, []byte(body))
	}

	// 分片正文是 OpenAI Chat Completions 的合法响应形状，必须取到正文而不是判空。
	output, err := finish(t, `{"choices":[{"message":{"role":"assistant","content":[{"type":"text","text":"真实回答"}]}}]}`)
	if err != nil {
		t.Fatalf("分片正文 error = %v, want the text to be extracted", err)
	}
	if output["text"] != "真实回答" {
		t.Fatalf("分片正文 output = %#v, want 真实回答", output)
	}

	// 多个分片按顺序拼接。
	output, err = finish(t, `{"choices":[{"message":{"content":[{"type":"text","text":"前"},{"type":"text","text":"后"}]}}]}`)
	if err != nil {
		t.Fatalf("多分片 error = %v", err)
	}
	if output["text"] != "前后" {
		t.Fatalf("多分片 output = %#v, want 前后", output)
	}

	// 字符串正文保持原行为。
	output, err = finish(t, `{"choices":[{"message":{"role":"assistant","content":"真实回答"}}]}`)
	if err != nil {
		t.Fatalf("字符串正文 error = %v", err)
	}
	if output["text"] != "真实回答" {
		t.Fatalf("字符串正文 output = %#v, want 真实回答", output)
	}

	// 正文既不在声明路径上也不是分片形状时，诊断必须点名接口类型和响应外形，
	// 且不能回显正文。
	unmapped := `{"answer":"真实回答","usage":{"total_tokens":12}}`
	_, err = finish(t, unmapped)
	if err == nil {
		t.Fatal("未命中路径的响应必须失败")
	}
	message := err.Error()
	if message == errProtocolResultEmpty.Error() {
		t.Fatalf("未命中路径被报成空响应：%q", message)
	}
	for _, want := range []string{"chat-completion", "顶层字段", "answer", "usage"} {
		if !strings.Contains(message, want) {
			t.Fatalf("诊断 %q 缺少 %q", message, want)
		}
	}
	if strings.Contains(message, "真实回答") {
		t.Fatalf("诊断回显了正文：%q", message)
	}

	// 只有推理、没有正文时不再返回空文本的「成功」。图片/视频/音频早就是失败语义，
	// 文本也不能让下游把空正文当成生成完成。
	if _, err := finish(t, `{"choices":[{"message":{"content":"","reasoning_content":"思考过程"}}]}`); err == nil {
		t.Fatal("只有推理的响应不能被当成生成成功")
	}
}

func TestProtocolMissingResultErrorReportsShape(t *testing.T) {
	if got := protocolMissingResultError("chat-completion", nil).Error(); got != errProtocolResultEmpty.Error() {
		t.Fatalf("empty body error = %q, want %q", got, errProtocolResultEmpty.Error())
	}
	if got := protocolMissingResultError("", []byte("   \n")).Error(); got != errProtocolResultEmpty.Error() {
		t.Fatalf("blank body error = %q, want %q", got, errProtocolResultEmpty.Error())
	}
	cases := []struct{ name, body, want string }{
		{"object", `{"code":0,"data":{"choices":[]}}`, "顶层字段 code、data"},
		{"empty object", `{}`, "JSON 对象没有字段"},
		{"array", `[1,2]`, "JSON 顶层不是对象"},
		{"not json", `<html>gateway error</html>`, "非 JSON 响应"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			message := protocolMissingResultError("some-plugin", []byte(test.body)).Error()
			if !strings.Contains(message, test.want) {
				t.Fatalf("shape diagnostic %q lost %q", message, test.want)
			}
			if !strings.Contains(message, "some-plugin") {
				t.Fatalf("shape diagnostic %q lost the interface type", message)
			}
			if !strings.Contains(message, "responseBody") {
				t.Fatalf("shape diagnostic %q lost the log pointer", message)
			}
			if strings.Contains(message, "gateway error") {
				t.Fatalf("shape diagnostic leaked the response body: %q", message)
			}
		})
	}
	// 未提供接口类型时不能输出空占位。
	if message := protocolMissingResultError("", []byte(`{"a":1}`)).Error(); !strings.Contains(message, "当前接口") {
		t.Fatalf("blank interface type diagnostic = %q", message)
	}
}
