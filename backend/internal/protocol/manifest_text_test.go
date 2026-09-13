package protocol

import "testing"

// OpenAI Chat Completions 的 message.content 既可能是字符串，也可能是分片数组。
// 只认字符串会把分片正文判成空结果，最终报成「已完成但没有返回结果」。
func TestFirstPathTextAcceptsContentParts(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{"string content", map[string]any{"content": "回答"}, "回答"},
		{"blank string", map[string]any{"content": "   "}, ""},
		{"null content", map[string]any{"content": nil}, ""},
		{
			"parts array",
			map[string]any{"content": []any{map[string]any{"type": "text", "text": "回答"}}},
			"回答",
		},
		{
			"multiple parts keep order",
			map[string]any{"content": []any{
				map[string]any{"type": "text", "text": "前"},
				map[string]any{"type": "text", "text": "后"},
			}},
			"前后",
		},
		{
			"parts skip non-text entries",
			map[string]any{"content": []any{
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/a.png"}},
				map[string]any{"type": "text", "text": "回答"},
				"裸字符串片段",
			}},
			"回答裸字符串片段",
		},
		{"parts without text", map[string]any{"content": []any{map[string]any{"type": "image_url"}}}, ""},
		{"object content is not text", map[string]any{"content": map[string]any{"text": "回答"}}, ""},
		{"numeric content is not text", map[string]any{"content": float64(3)}, ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := firstPathText(test.payload, "content"); got != test.want {
				t.Fatalf("firstPathText() = %q, want %q", got, test.want)
			}
		})
	}
}

// 声明式插件普遍把正文声明为 textPaths，抽取必须走分片兼容版本；
// 仍然只认字符串会让 content 为数组的响应变成空结果。
func TestFirstPathTextFallsBackAcrossPaths(t *testing.T) {
	payload := map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"content": []any{map[string]any{"text": "分片回答"}}}}},
	}
	if got := firstPathText(payload, "choices.0.message.content", "choices.0.text"); got != "分片回答" {
		t.Fatalf("firstPathText() = %q, want 分片回答", got)
	}
	if got := firstPathValue(payload, "choices.0.message.content"); got != "" {
		t.Fatalf("firstPathValue() = %q, want empty so the gap this fixes stays visible", got)
	}
}
