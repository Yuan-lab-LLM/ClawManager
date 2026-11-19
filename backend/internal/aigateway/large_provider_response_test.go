package aigateway

import (
	"strings"
	"testing"
)

func TestProxyOpenAIStreamAcceptsLargeProviderResponse(t *testing.T) {
	t.Run("large first reasoning frame", func(t *testing.T) {
		reasoning := strings.Repeat("r", 1<<20) + " reasoning end"
		input := `data: {"choices":[{"delta":{"reasoning":"` + reasoning + `"}}]}` + "\n\n" +
			"data: [DONE]\n\n"
		out := &flushBuffer{}

		result, err := proxyOpenAIStream(strings.NewReader(input), out, out, true)
		if err != nil {
			t.Fatal(err)
		}
		if result.RawStream != input || !result.SawDone || result.FirstTokenAt.IsZero() {
			t.Fatal("original provider stream, terminal marker or first token timestamp was lost")
		}
		if !strings.Contains(out.String(), `"reasoning_content":"`+reasoning+`"`) || !strings.HasSuffix(out.String(), "data: [DONE]\n\n") {
			t.Fatal("client stream is missing complete reasoning or terminal frame")
		}
	})

	t.Run("many smaller content frames", func(t *testing.T) {
		content := strings.Repeat("x", 64<<10) + " frame end"
		frame := `data: {"choices":[{"delta":{"content":"` + content + `"}}]}` + "\n\n"
		input := strings.Repeat(frame, 17) + `data: {"choices":[],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}` + "\n\n" +
			"data: [DONE]\n\n"
		out := &flushBuffer{}

		result, err := proxyOpenAIStream(strings.NewReader(input), out, out, true)
		if err != nil {
			t.Fatal(err)
		}
		if result.RawStream != input || out.String() != input || result.AssistantText != strings.Repeat(content, 17) {
			t.Fatal("stream or assistant content was truncated")
		}
		if !result.SawDone || result.PromptTokens != 2 || result.CompletionTokens != 3 || result.TotalTokens != 5 {
			t.Fatal("terminal marker or usage was lost")
		}
	})
}

func TestProxyAnthropicStreamAcceptsLargeProviderResponse(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
	}{
		{name: "first chunk exceeds deferred prelude", size: maxDeferredStreamPreludeBytes},
		{name: "first chunk exceeds former response limit", size: 1 << 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := strings.Repeat("x", tc.size) + " content end"
			input := "event: content_block_start\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}` + "\n\n" +
				"event: content_block_delta\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"` + content + `"}}` + "\n\n" +
				"event: message_stop\n" + `data: {"type":"message_stop"}` + "\n\n"
			out := &flushBuffer{}
			state := &anthropicStreamState{}

			raw, err := proxyAnthropicStream(strings.NewReader(input), out, out, state, true)
			if err != nil {
				t.Fatal(err)
			}
			if raw != input || state.AssistantText.String() != content || state.FirstTokenAt.IsZero() {
				t.Fatal("original provider stream, assistant content or first token timestamp was lost")
			}
			roleAt := strings.Index(out.String(), `"role":"assistant"`)
			contentAt := strings.Index(out.String(), `"content":"`+content+`"`)
			if roleAt < 0 || contentAt <= roleAt {
				t.Fatal("client stream did not receive role followed by complete content")
			}
		})
	}
}

func TestProxyOpenAIStreamForwardsLargePrelude(t *testing.T) {
	input := ": " + strings.Repeat("x", maxDeferredStreamPreludeBytes+1) + "\n\ndata: [DONE]\n\n"
	output := &flushBuffer{}

	result, err := proxyOpenAIStream(strings.NewReader(input), output, output, true)
	if err != nil || !result.SawDone || output.String() != input {
		t.Fatalf("long prelude interrupted an otherwise complete stream: sent=%d expected=%d err=%v", output.Len(), len(input), err)
	}
}
