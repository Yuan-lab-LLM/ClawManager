package aigateway

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services"
)

type observeStreamReader struct {
	io.Reader
	observe func()
}

func (r *observeStreamReader) Read(p []byte) (int, error) {
	if r.observe != nil {
		r.observe()
		r.observe = nil
	}
	return r.Reader.Read(p)
}

func TestReasoningStreamFlushesBeforeAnswerAndDoesNotPolluteAnswer(t *testing.T) {
	for _, field := range []string{"reasoning_content", "reasoning", "refusal"} {
		t.Run(field, func(t *testing.T) {
			first := fmt.Sprintf("data: {\"choices\":[{\"delta\":{\"%s\":\"progress\"}}]}\n\n", field)
			out := &flushBuffer{}
			var beforeAnswer time.Time
			tail := &observeStreamReader{Reader: strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"), observe: func() {
				beforeAnswer = time.Now()
				if out.Len() == 0 || out.flushes == 0 {
					t.Fatal("reasoning buffered until answer/EOF")
				}
			}}
			result, err := proxyOpenAIStream(io.MultiReader(strings.NewReader(first), tail), out, out, true)
			if err != nil || result.FirstTokenAt.IsZero() || result.AssistantText != "answer" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if result.FirstTokenAt.After(beforeAnswer) {
				t.Fatal("TTFT measured the answer instead of initial reasoning")
			}
			if field == "reasoning" && !strings.Contains(out.String(), `"reasoning_content":"progress"`) {
				t.Fatal("client alias missing")
			}
		})
	}
}

func TestReasoningStreamExceedsPreludeLimitWithoutFailing(t *testing.T) {
	for _, field := range []string{"reasoning_content", "reasoning"} {
		for _, size := range []int{64, maxDeferredStreamPreludeBytes + 1} {
			t.Run(fmt.Sprintf("%s/%d", field, size), func(t *testing.T) {
				chunk := fmt.Sprintf("data: {\"choices\":[{\"delta\":{\"%s\":\"%s\"}}]}\n\n", field, strings.Repeat("x", size))
				count := maxDeferredStreamPreludeBytes/len(chunk) + 2
				input := strings.Repeat(chunk, count) + "data: [DONE]\n\n"
				out := &flushBuffer{}
				result, err := proxyOpenAIStream(strings.NewReader(input), out, out, true)
				if err != nil || result.FirstTokenAt.IsZero() || !result.Terminal || strings.Count(out.String(), `"reasoning_content"`) != count || !strings.Contains(out.String(), "data: [DONE]") {
					t.Fatalf("thinking stream rejected: %v", err)
				}
			})
		}
	}
}

func TestReasoningStreamEmptyOrMalformedThinkingIsNotProgress(t *testing.T) {
	for _, delta := range []string{`{"reasoning_content":""}`, `{"reasoning":null}`, `{"reasoning":{"signature":"opaque"}}`, `{"reasoning":42}`, `{"role":"assistant"}`} {
		out := &flushBuffer{}
		result, err := proxyOpenAIStream(strings.NewReader("data: {\"choices\":[{\"delta\":"+delta+"}]}\n\n"), out, out, true)
		if err == nil || !result.FirstTokenAt.IsZero() || out.Len() != 0 {
			t.Fatalf("invalid progress: %s result=%+v error=%v", delta, result, err)
		}
	}
}

func TestReasoningStreamAliasFillsEmptyStandardField(t *testing.T) {
	for _, empty := range []string{`null`, `""`} {
		line := `data: {"choices":[{"delta":{"reasoning":"progress","reasoning_content":` + empty + `}}]}` + "\n"
		if got := normalizeOpenAIReasoningDelta(line); !strings.Contains(got, `"reasoning_content":"progress"`) {
			t.Fatal(got)
		}
	}
}

func TestReasoningStreamNeverSwitchesAfterThinkingIsVisible(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"progress\"}}]}\n\n")
	})) // abruptly truncated after thinking
	defer first.Close()
	secondCalls := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { secondCalls++; w.WriteHeader(200) }))
	defer second.Close()
	audit := &stubAuditEventService{}
	gateway := &service{invocationService: &stubModelInvocationService{}, auditEventService: audit, secretRefService: services.NewSecretRefService(), streamHTTPClient: first.Client()}
	prepared := &preparedChatRequest{traceID: "trace", requestID: "request", req: ChatCompletionRequest{Model: "auto", Messages: []ChatMessage{{Role: "user", Content: "hello"}}, Stream: true}, deploymentCandidates: []*models.LLMModel{
		{ID: 1, ProviderType: models.ProviderTypeOpenAICompatible, ProviderModelName: "primary", BaseURL: first.URL},
		{ID: 2, ProviderType: models.ProviderTypeOpenAICompatible, ProviderModelName: "fallback", BaseURL: second.URL},
	}}
	out := httptest.NewRecorder()
	err := gateway.streamPreparedChatCompletions(t.Context(), prepared, out)
	if err == nil || secondCalls != 0 {
		t.Fatalf("must surface truncation without mixing models: err=%v fallback=%d", err, secondCalls)
	}
	if !strings.Contains(out.Body.String(), "reasoning_content") || !strings.Contains(out.Body.String(), "upstream_stream_error") {
		t.Fatal("thinking or terminal error missing")
	}
	found := false
	for _, event := range audit.events {
		if event.EventType == "gateway.request.stream_failed" {
			found = true
		}
	}
	if !found {
		t.Fatal("final failure event missing")
	}
}
