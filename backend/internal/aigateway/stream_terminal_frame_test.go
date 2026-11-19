package aigateway

import (
	"strings"
	"testing"
)

func TestProxyOpenAIStreamCompletesTerminalFrame(t *testing.T) {
	for _, line := range []string{"data: [DONE]\n\n", "data: [DONE]\r\n\r\n", "data: [DONE]", "data: [DONE]\n"} {
		t.Run(strings.ReplaceAll(line, "\n", "LF"), func(t *testing.T) {
			out := &flushBuffer{}
			result, err := proxyOpenAIStream(strings.NewReader(line), out, out)
			if err != nil || !result.Terminal {
				t.Fatalf("terminal result %+v %v", result, err)
			}
			expected := line
			if out.String() != expected || result.RawStream != expected {
				t.Fatalf("incomplete terminal SSE: output %q durable %q", out.String(), result.RawStream)
			}
		})
	}
}
