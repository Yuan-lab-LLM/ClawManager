package aigateway

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	upstreamStreamInterruptedCode = "upstream_stream_interrupted"
	maxDeferredStreamPreludeBytes = 256 * 1024
)

type openAIStreamChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object,omitempty"`
	Created int64  `json:"created,omitempty"`
	Model   string `json:"model,omitempty"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role             string      `json:"role,omitempty"`
			Content          interface{} `json:"content,omitempty"`
			ToolCalls        []ToolCall  `json:"tool_calls,omitempty"`
			Refusal          interface{} `json:"refusal,omitempty"`
			ReasoningContent interface{} `json:"reasoning_content,omitempty"`
			Reasoning        interface{} `json:"reasoning,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason,omitempty"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
	Error *struct {
		Message string `json:"message,omitempty"`
		Type    string `json:"type,omitempty"`
		Code    string `json:"code,omitempty"`
	} `json:"error,omitempty"`
}

type openAIStreamInspection struct {
	Done          bool
	Finished      bool
	Semantic      bool
	ProviderError error
}

type openAIStreamProxyResult struct {
	RawStream        string
	AssistantText    string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	SawDone          bool
	SawFinishReason  bool
	Terminal         bool
	FirstTokenAt     time.Time
}

type upstreamStreamReadError struct {
	err error
}

func (e *upstreamStreamReadError) Error() string {
	return fmt.Sprintf("failed while reading provider stream: %v", e.err)
}

func (e *upstreamStreamReadError) Unwrap() error {
	return e.err
}

type upstreamStreamProviderError struct {
	message string
}

func (e *upstreamStreamProviderError) Error() string {
	return e.message
}

type downstreamStreamWriteError struct {
	err error
}

func (e *downstreamStreamWriteError) Error() string {
	return fmt.Sprintf("failed while writing provider stream: %v", e.err)
}

func (e *downstreamStreamWriteError) Unwrap() error {
	return e.err
}

// deferredStreamWriter holds protocol prelude/heartbeat frames until either a
// semantic token, a valid terminal event, or the buffering budget is reached.
// Committing the prelude bounds memory and disables failover without rejecting
// the provider's stream.
type deferredStreamWriter struct {
	writer    io.Writer
	flusher   http.Flusher
	pending   bytes.Buffer
	committed bool
}

func (w *deferredStreamWriter) Write(payload []byte) (int, error) {
	if w.committed {
		return w.writer.Write(payload)
	}
	if len(payload) > maxDeferredStreamPreludeBytes-w.pending.Len() {
		if err := w.Commit(); err != nil {
			return 0, err
		}
		return w.writer.Write(payload)
	}
	return w.pending.Write(payload)
}

func (w *deferredStreamWriter) Flush() {
	if w.committed {
		w.flusher.Flush()
	}
}

func (w *deferredStreamWriter) Commit() error {
	if w.committed {
		return nil
	}
	w.committed = true
	if w.pending.Len() > 0 {
		if _, err := w.writer.Write(w.pending.Bytes()); err != nil {
			return err
		}
		w.pending.Reset()
	}
	w.flusher.Flush()
	return nil
}

type openAIStreamDelta struct {
	Role      string      `json:"role,omitempty"`
	Content   interface{} `json:"content,omitempty"`
	ToolCalls []ToolCall  `json:"tool_calls,omitempty"`
}

type openAIStreamChoice struct {
	Index        int               `json:"index"`
	Delta        openAIStreamDelta `json:"delta"`
	FinishReason *string           `json:"finish_reason,omitempty"`
}

type openAIStreamResponseChunk struct {
	ID      string               `json:"id"`
	Object  string               `json:"object,omitempty"`
	Created int64                `json:"created,omitempty"`
	Model   string               `json:"model,omitempty"`
	Choices []openAIStreamChoice `json:"choices"`
	Usage   *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

type anthropicStreamEvent struct {
	Type         string                    `json:"type"`
	Message      *anthropicMessageResponse `json:"message,omitempty"`
	Index        *int                      `json:"index,omitempty"`
	ContentBlock *anthropicContentBlock    `json:"content_block,omitempty"`
	Delta        *struct {
		Type         string `json:"type,omitempty"`
		Text         string `json:"text,omitempty"`
		PartialJSON  string `json:"partial_json,omitempty"`
		StopReason   string `json:"stop_reason,omitempty"`
		StopSequence string `json:"stop_sequence,omitempty"`
	} `json:"delta,omitempty"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage,omitempty"`
	Error *struct {
		Type    string `json:"type,omitempty"`
		Message string `json:"message,omitempty"`
	} `json:"error,omitempty"`
}

type anthropicStreamState struct {
	ResponseID       string
	Model            string
	Created          int64
	PromptTokens     int
	CompletionTokens int
	AssistantText    strings.Builder
	ToolCalls        map[int]*anthropicToolCallState
	SentRole         bool
	FirstTokenAt     time.Time
}

type anthropicToolCallState struct {
	ID        string
	Name      string
	Arguments strings.Builder
}

func proxyAnthropicStream(reader io.Reader, writer io.Writer, flusher http.Flusher, state *anthropicStreamState, deferUntilSemantic ...bool) (string, error) {
	buffered := bufio.NewReader(reader)
	var deferred *deferredStreamWriter
	if len(deferUntilSemantic) > 0 && deferUntilSemantic[0] {
		deferred = &deferredStreamWriter{writer: writer, flusher: flusher}
		writer, flusher = deferred, deferred
	}
	var rawStream strings.Builder
	eventType := ""
	dataLines := make([]string, 0, 2)
	done := false

	emitChunk := func(chunk openAIStreamResponseChunk) error {
		// Only prelude data belongs in the failover buffer. A translated
		// semantic chunk can be larger than that buffer on its own.
		if deferred != nil && !state.FirstTokenAt.IsZero() {
			if err := deferred.Commit(); err != nil {
				return &downstreamStreamWriteError{err: err}
			}
		}
		return emitOpenAIStreamPayload(writer, flusher, chunk)
	}

	flushEvent := func() error {
		if len(dataLines) == 0 {
			eventType = ""
			return nil
		}
		payload := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		finished, err := processAnthropicStreamEvent(payload, eventType, state, emitChunk)
		eventType = ""
		if err != nil {
			return err
		}
		if finished {
			done = true
		}
		if deferred != nil && finished {
			if err := deferred.Commit(); err != nil {
				return &downstreamStreamWriteError{err: err}
			}
		}
		return nil
	}

	for !done {
		line, readErr := buffered.ReadString('\n')
		if line != "" {
			rawStream.WriteString(line)
			trimmedLine := strings.TrimRight(line, "\r\n")
			switch {
			case trimmedLine == "":
				if err := flushEvent(); err != nil {
					return rawStream.String(), err
				}
			case strings.HasPrefix(trimmedLine, "event:"):
				eventType = strings.TrimSpace(strings.TrimPrefix(trimmedLine, "event:"))
			case strings.HasPrefix(trimmedLine, "data:"):
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(trimmedLine, "data:")))
			}
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				if err := flushEvent(); err != nil {
					return rawStream.String(), err
				}
				if done {
					return rawStream.String(), nil
				}
				return rawStream.String(), &upstreamStreamReadError{err: io.ErrUnexpectedEOF}
			}
			return rawStream.String(), &upstreamStreamReadError{err: readErr}
		}
	}
	return rawStream.String(), nil
}

func proxyOpenAIStream(reader io.Reader, writer io.Writer, flusher http.Flusher, deferUntilSemantic ...bool) (openAIStreamProxyResult, error) {
	buffered := bufio.NewReader(reader)
	var deferred *deferredStreamWriter
	if len(deferUntilSemantic) > 0 && deferUntilSemantic[0] {
		deferred = &deferredStreamWriter{writer: writer, flusher: flusher}
		writer, flusher = deferred, deferred
	}
	var rawStream strings.Builder
	var assistantText strings.Builder
	promptTokens := 0
	completionTokens := 0
	totalTokens := 0
	sawDone := false
	sawFinishReason := false
	var pendingProviderError error
	var firstTokenAt time.Time

	result := func() openAIStreamProxyResult {
		return openAIStreamProxyResult{
			RawStream:        rawStream.String(),
			AssistantText:    assistantText.String(),
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
			SawDone:          sawDone,
			SawFinishReason:  sawFinishReason,
			Terminal:         sawDone || sawFinishReason,
			FirstTokenAt:     firstTokenAt,
		}
	}

	for {
		line, readErr := buffered.ReadString('\n')
		if line != "" {
			inspection := inspectStreamLine(
				line,
				&assistantText,
				&promptTokens,
				&completionTokens,
				&totalTokens,
			)
			sawDone = sawDone || inspection.Done
			sawFinishReason = sawFinishReason || inspection.Finished
			if inspection.Semantic && firstTokenAt.IsZero() {
				firstTokenAt = time.Now()
			}
			rawStream.WriteString(line)
			// A few OpenAI-compatible providers emit chain-of-thought deltas as
			// `delta.reasoning`. OpenCode and other clients generally consume the
			// standard `delta.reasoning_content` spelling instead. Keep the
			// provider payload in the audit stream unchanged, but add the standard
			// alias on the downstream stream so clients can finish their thinking
			// state and render the final content.
			downstreamLine := normalizeOpenAIReasoningDelta(line)
			// Commit BEFORE writing meaningful data: a single legitimate reasoning
			// chunk may itself exceed the bounded heartbeat/prelude buffer.
			if deferred != nil && pendingProviderError == nil && inspection.ProviderError == nil && (inspection.Semantic || inspection.Finished || inspection.Done) {
				if err := deferred.Commit(); err != nil {
					return result(), &downstreamStreamWriteError{err: err}
				}
			}
			if _, err := io.WriteString(writer, downstreamLine); err != nil {
				return result(), &downstreamStreamWriteError{err: err}
			}
			flusher.Flush()
			if inspection.ProviderError != nil {
				pendingProviderError = inspection.ProviderError
			}
			if pendingProviderError != nil && strings.TrimSpace(line) == "" {
				return result(), pendingProviderError
			}
			// DONE is an SSE data line; its following blank line terminates the
			// frame. Forward that delimiter before returning, without waiting for
			// the provider to close an otherwise complete stream.
			if sawDone && strings.TrimSpace(line) == "" {
				return result(), nil
			}
		}

		if readErr != nil {
			if pendingProviderError != nil {
				return result(), pendingProviderError
			}
			if errors.Is(readErr, io.EOF) {
				if sawDone || sawFinishReason {
					return result(), nil
				}
				return result(), &upstreamStreamReadError{err: io.ErrUnexpectedEOF}
			}
			return result(), &upstreamStreamReadError{err: readErr}
		}
	}
}

// normalizeOpenAIReasoningDelta adds the OpenAI-compatible reasoning_content
// alias to provider chunks that only expose reasoning. Invalid/non-data SSE
// lines and chunks that already contain reasoning_content are returned byte
// for byte unchanged.
func normalizeOpenAIReasoningDelta(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "data:") {
		return line
	}
	payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if payload == "" || payload == "[DONE]" || !strings.HasPrefix(payload, "{") {
		return line
	}

	var envelope map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		return line
	}
	choices, ok := envelope["choices"].([]interface{})
	if !ok {
		return line
	}
	changed := false
	for _, rawChoice := range choices {
		choice, ok := rawChoice.(map[string]interface{})
		if !ok {
			continue
		}
		delta, ok := choice["delta"].(map[string]interface{})
		if !ok {
			continue
		}
		if existing := delta["reasoning_content"]; existing != nil && existing != "" {
			continue
		}
		if reasoning, ok := delta["reasoning"].(string); ok && reasoning != "" {
			delta["reasoning_content"] = reasoning
			changed = true
		}
	}
	if !changed {
		return line
	}

	encoded, err := json.Marshal(envelope)
	if err != nil {
		return line
	}
	// Preserve the SSE line ending style expected by the caller.
	ending := ""
	if strings.HasSuffix(line, "\r\n") {
		ending = "\r\n"
	} else if strings.HasSuffix(line, "\n") {
		ending = "\n"
	}
	return "data: " + string(encoded) + ending
}

func inspectStreamLine(line string, assistantText *strings.Builder, promptTokens, completionTokens, totalTokens *int) openAIStreamInspection {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "data:") {
		return openAIStreamInspection{}
	}

	payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if payload == "" {
		return openAIStreamInspection{}
	}
	if payload == "[DONE]" {
		return openAIStreamInspection{Done: true}
	}

	var chunk openAIStreamChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return openAIStreamInspection{}
	}
	if chunk.Error != nil {
		message := strings.TrimSpace(chunk.Error.Message)
		if message == "" {
			message = "provider returned an error stream event"
		}
		return openAIStreamInspection{
			ProviderError: &upstreamStreamProviderError{message: message},
		}
	}

	if chunk.Usage != nil {
		*promptTokens = chunk.Usage.PromptTokens
		*completionTokens = chunk.Usage.CompletionTokens
		*totalTokens = chunk.Usage.TotalTokens
	}

	finished := false
	semantic := false
	for _, choice := range chunk.Choices {
		content := flattenMessageContent(choice.Delta.Content)
		if content != "" {
			assistantText.WriteString(content)
			semantic = true
		}
		if len(choice.Delta.ToolCalls) > 0 {
			semantic = true
		}
		// Reasoning is visible progress, not a heartbeat. Do not append it to
		// AssistantText: that field represents the final answer, not thinking.
		for _, value := range []interface{}{choice.Delta.ReasoningContent, choice.Delta.Reasoning, choice.Delta.Refusal} {
			if text, ok := value.(string); ok && text != "" {
				semantic = true
			}
		}
		if choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != "" {
			finished = true
		}
	}
	return openAIStreamInspection{Finished: finished, Semantic: semantic}
}

func processAnthropicStreamEvent(payload, eventType string, state *anthropicStreamState, emitChunk func(openAIStreamResponseChunk) error) (bool, error) {
	if strings.TrimSpace(payload) == "" {
		return false, nil
	}

	var event anthropicStreamEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return false, nil
	}
	if strings.TrimSpace(eventType) == "" {
		eventType = event.Type
	}

	switch eventType {
	case "message_start":
		if event.Message != nil {
			state.ResponseID = event.Message.ID
			state.Model = event.Message.Model
			state.PromptTokens = event.Message.Usage.InputTokens
		}
	case "content_block_start":
		if event.ContentBlock == nil || event.Index == nil {
			return false, nil
		}
		index := *event.Index
		switch event.ContentBlock.Type {
		case "text":
			chunk := openAIStreamResponseChunk{
				ID:      defaultIfBlank(state.ResponseID, "chatcmpl-anthropic"),
				Object:  "chat.completion.chunk",
				Created: state.Created,
				Model:   state.Model,
				Choices: []openAIStreamChoice{
					{
						Index: 0,
						Delta: openAIStreamDelta{
							Role: conditionalAssistantRole(state),
						},
					},
				},
			}
			if err := emitChunk(chunk); err != nil {
				return false, err
			}
		case "tool_use":
			if state.FirstTokenAt.IsZero() {
				state.FirstTokenAt = time.Now()
			}
			toolState := &anthropicToolCallState{
				ID:   defaultIfBlank(event.ContentBlock.ID, fmt.Sprintf("call_%d", index)),
				Name: strings.TrimSpace(event.ContentBlock.Name),
			}
			state.ToolCalls[index] = toolState
			toolCallIndex := index
			chunk := openAIStreamResponseChunk{
				ID:      defaultIfBlank(state.ResponseID, "chatcmpl-anthropic"),
				Object:  "chat.completion.chunk",
				Created: state.Created,
				Model:   state.Model,
				Choices: []openAIStreamChoice{
					{
						Index: 0,
						Delta: openAIStreamDelta{
							Role: conditionalAssistantRole(state),
							ToolCalls: []ToolCall{
								{
									ID:    toolState.ID,
									Type:  "function",
									Index: &toolCallIndex,
									Function: &ToolCallFunction{
										Name:      toolState.Name,
										Arguments: "",
									},
								},
							},
						},
					},
				},
			}
			if err := emitChunk(chunk); err != nil {
				return false, err
			}
		}
	case "content_block_delta":
		if event.Index == nil || event.Delta == nil {
			return false, nil
		}
		index := *event.Index
		switch event.Delta.Type {
		case "text_delta":
			if event.Delta.Text == "" {
				return false, nil
			}
			state.AssistantText.WriteString(event.Delta.Text)
			if state.FirstTokenAt.IsZero() {
				state.FirstTokenAt = time.Now()
			}
			chunk := openAIStreamResponseChunk{
				ID:      defaultIfBlank(state.ResponseID, "chatcmpl-anthropic"),
				Object:  "chat.completion.chunk",
				Created: state.Created,
				Model:   state.Model,
				Choices: []openAIStreamChoice{
					{
						Index: 0,
						Delta: openAIStreamDelta{
							Content: event.Delta.Text,
						},
					},
				},
			}
			if err := emitChunk(chunk); err != nil {
				return false, err
			}
		case "input_json_delta":
			toolState := state.ToolCalls[index]
			if toolState == nil {
				return false, nil
			}
			toolState.Arguments.WriteString(event.Delta.PartialJSON)
			toolCallIndex := index
			chunk := openAIStreamResponseChunk{
				ID:      defaultIfBlank(state.ResponseID, "chatcmpl-anthropic"),
				Object:  "chat.completion.chunk",
				Created: state.Created,
				Model:   state.Model,
				Choices: []openAIStreamChoice{
					{
						Index: 0,
						Delta: openAIStreamDelta{
							ToolCalls: []ToolCall{
								{
									ID:    toolState.ID,
									Type:  "function",
									Index: &toolCallIndex,
									Function: &ToolCallFunction{
										Arguments: event.Delta.PartialJSON,
									},
								},
							},
						},
					},
				},
			}
			if err := emitChunk(chunk); err != nil {
				return false, err
			}
		}
	case "message_delta":
		if event.Usage != nil {
			state.CompletionTokens = event.Usage.OutputTokens
		}
		if event.Delta != nil {
			finishReason := normalizeAnthropicStopReason(event.Delta.StopReason)
			if finishReason != "" {
				chunk := openAIStreamResponseChunk{
					ID:      defaultIfBlank(state.ResponseID, "chatcmpl-anthropic"),
					Object:  "chat.completion.chunk",
					Created: state.Created,
					Model:   state.Model,
					Choices: []openAIStreamChoice{
						{
							Index:        0,
							Delta:        openAIStreamDelta{},
							FinishReason: &finishReason,
						},
					},
				}
				if err := emitChunk(chunk); err != nil {
					return false, err
				}
			}
		}
	case "message_stop":
		return true, nil
	case "error":
		if event.Error != nil && strings.TrimSpace(event.Error.Message) != "" {
			return false, errors.New(event.Error.Message)
		}
	}

	return false, nil
}

func emitOpenAIStreamPayload(w io.Writer, flusher http.Flusher, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func emitOpenAIStreamDone(w io.Writer, flusher http.Flusher) error {
	if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func emitOpenAIStreamError(w io.Writer, flusher http.Flusher, traceID string, _ error) error {
	payload := struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
			TraceID string `json:"trace_id,omitempty"`
		} `json:"error"`
	}{}
	payload.Error.Message = upstreamStreamInterruptedCode + ": upstream model stream ended before a verified terminal event; retry this turn"
	payload.Error.Type = "upstream_stream_error"
	payload.Error.Code = upstreamStreamInterruptedCode
	payload.Error.TraceID = strings.TrimSpace(traceID)
	return emitOpenAIStreamPayload(w, flusher, payload)
}

func conditionalAssistantRole(state *anthropicStreamState) string {
	if state.SentRole {
		return ""
	}
	state.SentRole = true
	return "assistant"
}

func renderAnthropicToolCalls(state *anthropicStreamState) string {
	if len(state.ToolCalls) == 0 {
		return ""
	}

	indexes := make([]int, 0, len(state.ToolCalls))
	for index := range state.ToolCalls {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)

	parts := make([]string, 0, len(state.ToolCalls))
	for _, index := range indexes {
		toolState := state.ToolCalls[index]
		if toolState == nil {
			continue
		}
		args := strings.TrimSpace(toolState.Arguments.String())
		switch {
		case toolState.Name != "" && args != "":
			parts = append(parts, fmt.Sprintf("tool_call %s(%s)", toolState.Name, args))
		case toolState.Name != "":
			parts = append(parts, "tool_call "+toolState.Name)
		}
	}
	return strings.Join(parts, "\n")
}
