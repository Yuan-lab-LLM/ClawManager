package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"clawreef/internal/aigateway"
	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

// Responses translates the public Responses protocol to the gateway's governed
// Chat Completions request.  Keeping the translation at the edge means the
// router, auditing and provider adapters remain shared by all runtimes.
func (h *AIGatewayHandler) Responses(c *gin.Context) {
	var payload responsesRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		utils.ValidationError(c, err)
		return
	}
	req := payload.chatRequest()
	if !applyGatewayRequestContext(c, &req) {
		return
	}
	input, err := h.resolveWorkspaceInputFiles(c.Request.Context(), c, payload.Input)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	req.Messages = responseInputMessages(input)
	if strings.TrimSpace(payload.Instructions) != "" {
		req.Messages = append([]aigateway.ChatMessage{{Role: "system", Content: payload.Instructions}}, req.Messages...)
	}
	req.RawBody = canonicalChatBody(req)
	if payload.Stream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		traceID, err := h.service.StreamChatCompletions(c.Request.Context(), gatewayUserID(c), req, &protocolStreamWriter{ResponseWriter: c.Writer, kind: "responses", model: payload.Model})
		if traceID != "" {
			c.Header("X-Trace-ID", traceID)
		}
		if err != nil && !c.Writer.Written() {
			utils.HandleError(c, err)
		}
		return
	}
	response, traceID, err := h.service.ChatCompletions(c.Request.Context(), gatewayUserID(c), req)
	if err != nil {
		c.Header("X-Trace-ID", traceID)
		utils.HandleError(c, err)
		return
	}
	c.Header("X-Trace-ID", traceID)
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		c.JSON(response.StatusCode, responsesError(response.Body))
		return
	}
	c.JSON(response.StatusCode, responseToResponses(traceID, payload.Model, response.Body))
}

const maxGatewayWorkspaceFileBytes = int64(2 * 1024 * 1024)
const maxGatewayWorkspaceFiles = 5
const maxGatewayWorkspaceInputBytes = int64(8 * 1024 * 1024)

// resolveWorkspaceInputFiles accepts only workspace://relative/path references.
// It deliberately never follows URLs or absolute paths from an agent request.
func (h *AIGatewayHandler) resolveWorkspaceInputFiles(ctx context.Context, c *gin.Context, input interface{}) (interface{}, error) {
	items, ok := input.([]interface{})
	if !ok {
		return input, nil
	}
	files, totalBytes := 0, int64(0)
	for _, raw := range items {
		message, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := message["content"].([]interface{})
		if !ok {
			continue
		}
		for index, item := range content {
			block, ok := item.(map[string]interface{})
			if !ok || block["type"] != "input_file" {
				continue
			}
			files++
			if files > maxGatewayWorkspaceFiles {
				return nil, fmt.Errorf("input_file count exceeds %d", maxGatewayWorkspaceFiles)
			}
			fileRef, _ := block["file_url"].(string)
			if fileRef == "" {
				fileRef, _ = block["file_id"].(string)
			}
			text, err := h.readWorkspaceInputFile(ctx, c, fileRef)
			if err != nil {
				return nil, err
			}
			totalBytes += int64(len(text))
			if totalBytes > maxGatewayWorkspaceInputBytes {
				return nil, fmt.Errorf("combined input_file content exceeds %d byte limit", maxGatewayWorkspaceInputBytes)
			}
			content[index] = map[string]interface{}{"type": "input_text", "text": text}
		}
	}
	return input, nil
}

func (h *AIGatewayHandler) readWorkspaceInputFile(ctx context.Context, c *gin.Context, reference string) (string, error) {
	if h.instanceService == nil || h.workspaceFileService == nil || h.runtimeWorkspaceFileService == nil {
		return "", fmt.Errorf("workspace file input is not configured")
	}
	instanceID, ok := c.Get("instanceID")
	if !ok {
		return "", fmt.Errorf("input_file requires an instance token")
	}
	instanceIDValue, ok := instanceID.(int)
	if !ok || instanceIDValue <= 0 {
		return "", fmt.Errorf("input_file requires an instance token")
	}
	relative, err := workspaceInputFilePath(reference, instanceIDValue)
	if err != nil {
		return "", err
	}
	instance, err := h.instanceService.GetByID(instanceIDValue)
	if err != nil || instance == nil {
		return "", fmt.Errorf("workspace instance is unavailable")
	}
	service := h.workspaceFileService
	scope := services.WorkspaceFileScope{InstanceID: instance.ID, UserID: instance.UserID, WorkspacePath: "/config", AuditActionPrefix: "llm_"}
	if !isDesktopWorkspaceInstance(instance) {
		if instance.WorkspacePath == nil || strings.TrimSpace(*instance.WorkspacePath) == "" {
			return "", fmt.Errorf("workspace is unavailable")
		}
		scope.WorkspacePath = *instance.WorkspacePath
	} else {
		service = h.runtimeWorkspaceFileService
	}
	file, name, size, err := service.OpenDownload(ctx, scope, relative)
	if err != nil {
		return "", fmt.Errorf("open workspace input_file: %w", err)
	}
	defer file.Close()
	if size > maxGatewayWorkspaceFileBytes {
		return "", fmt.Errorf("input_file exceeds %d byte limit", maxGatewayWorkspaceFileBytes)
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt", ".md", ".markdown":
	default:
		return "", fmt.Errorf("input_file type %q is not supported yet", filepath.Ext(name))
	}
	data, err := io.ReadAll(io.LimitReader(file, maxGatewayWorkspaceFileBytes+1))
	if err != nil {
		return "", fmt.Errorf("read workspace input_file: %w", err)
	}
	if int64(len(data)) > maxGatewayWorkspaceFileBytes {
		return "", fmt.Errorf("input_file exceeds %d byte limit", maxGatewayWorkspaceFileBytes)
	}
	return fmt.Sprintf("[Workspace file: %s]\n%s", filepath.ToSlash(relative), string(data)), nil
}

func workspaceInputFilePath(reference string, instanceID int) (string, error) {
	reference = strings.TrimSpace(reference)
	pathValue := ""
	switch {
	case strings.HasPrefix(reference, "workspace://"):
		pathValue = strings.TrimPrefix(reference, "workspace://")
	case strings.HasPrefix(reference, "workspace:"):
		pathValue = strings.TrimPrefix(reference, "workspace:")
	case strings.HasPrefix(reference, "/api/v1/instances/"):
		parsed, err := url.Parse(reference)
		if err != nil || parsed.IsAbs() {
			return "", fmt.Errorf("input_file URL is invalid")
		}
		prefix := fmt.Sprintf("/api/v1/instances/%d/workspace/download", instanceID)
		if parsed.Path != prefix {
			return "", fmt.Errorf("input_file URL must target the current instance workspace")
		}
		pathValue = parsed.Query().Get("path")
	default:
		return "", fmt.Errorf("input_file must use a workspace reference")
	}
	pathValue = strings.TrimSpace(filepath.ToSlash(pathValue))
	if pathValue == "" || filepath.IsAbs(pathValue) || strings.Contains(pathValue, "../") || pathValue == ".." {
		return "", fmt.Errorf("input_file path must stay within the instance workspace")
	}
	return pathValue, nil
}

// AnthropicMessages translates Messages requests, including tool_use and
// tool_result blocks, to the same internal tool-call representation.
func (h *AIGatewayHandler) AnthropicMessages(c *gin.Context) {
	var payload anthropicRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		utils.ValidationError(c, err)
		return
	}
	req := payload.chatRequest()
	if !applyGatewayRequestContext(c, &req) {
		return
	}
	if payload.Stream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		traceID, err := h.service.StreamChatCompletions(c.Request.Context(), gatewayUserID(c), req, &protocolStreamWriter{ResponseWriter: c.Writer, kind: "anthropic", model: payload.Model})
		if traceID != "" {
			c.Header("X-Trace-ID", traceID)
		}
		if err != nil && !c.Writer.Written() {
			utils.HandleError(c, err)
		}
		return
	}
	response, traceID, err := h.service.ChatCompletions(c.Request.Context(), gatewayUserID(c), req)
	if err != nil {
		c.Header("X-Trace-ID", traceID)
		utils.HandleError(c, err)
		return
	}
	c.Header("X-Trace-ID", traceID)
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		c.JSON(response.StatusCode, anthropicError(response.Body))
		return
	}
	c.JSON(response.StatusCode, responseToAnthropic(traceID, payload.Model, response.Body))
}

type responsesRequest struct {
	Model             string          `json:"model"`
	Input             interface{}     `json:"input"`
	Instructions      string          `json:"instructions"`
	Tools             json.RawMessage `json:"tools"`
	ToolChoice        json.RawMessage `json:"tool_choice"`
	Stream            bool            `json:"stream"`
	MaxOutputTokens   *int            `json:"max_output_tokens"`
	Temperature       *float64        `json:"temperature"`
	TopP              *float64        `json:"top_p"`
	ParallelToolCalls *bool           `json:"parallel_tool_calls"`
	Reasoning         struct {
		Effort string `json:"effort"`
	} `json:"reasoning"`
	Text struct {
		Format json.RawMessage `json:"format"`
	} `json:"text"`
}
type anthropicRequest struct {
	Model    string      `json:"model"`
	System   interface{} `json:"system"`
	Messages []struct {
		Role    string      `json:"role"`
		Content interface{} `json:"content"`
	} `json:"messages"`
	Tools         json.RawMessage `json:"tools"`
	ToolChoice    json.RawMessage `json:"tool_choice"`
	Stream        bool            `json:"stream"`
	MaxTokens     *int            `json:"max_tokens"`
	Temperature   *float64        `json:"temperature"`
	TopP          *float64        `json:"top_p"`
	StopSequences json.RawMessage `json:"stop_sequences"`
}

func (p responsesRequest) chatRequest() aigateway.ChatCompletionRequest {
	messages := make([]aigateway.ChatMessage, 0)
	if strings.TrimSpace(p.Instructions) != "" {
		messages = append(messages, aigateway.ChatMessage{Role: "system", Content: p.Instructions})
	}
	messages = append(messages, responseInputMessages(p.Input)...)
	req := aigateway.ChatCompletionRequest{Model: p.Model, Messages: messages, Stream: p.Stream, Tools: responsesTools(p.Tools), ToolChoice: p.ToolChoice, MaxTokens: p.MaxOutputTokens, Temperature: p.Temperature, TopP: p.TopP, ParallelToolCalls: p.ParallelToolCalls, ResponseFormat: responseTextFormat(p.Text.Format)}
	if effort := strings.TrimSpace(p.Reasoning.Effort); effort != "" {
		req.ReasoningEffort = &effort
	}
	req.RawBody = canonicalChatBody(req)
	return req
}
func responseTextFormat(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var format map[string]interface{}
	if json.Unmarshal(raw, &format) != nil {
		return nil
	}
	// Responses nests the Chat Completions-compatible response format under text.format.
	if _, ok := format["type"]; !ok {
		return nil
	}
	result, _ := json.Marshal(format)
	return result
}
func (p anthropicRequest) chatRequest() aigateway.ChatCompletionRequest {
	messages := make([]aigateway.ChatMessage, 0, len(p.Messages)+1)
	if text := protocolBlockText(p.System); text != "" {
		messages = append(messages, aigateway.ChatMessage{Role: "system", Content: text})
	}
	for _, item := range p.Messages {
		messages = append(messages, anthropicMessages(item.Role, item.Content)...)
	}
	req := aigateway.ChatCompletionRequest{Model: p.Model, Messages: messages, Stream: p.Stream, Tools: anthropicTools(p.Tools), ToolChoice: anthropicToolChoice(p.ToolChoice), MaxTokens: p.MaxTokens, Temperature: p.Temperature, TopP: p.TopP, Stop: p.StopSequences}
	req.RawBody = canonicalChatBody(req)
	return req
}

func canonicalChatBody(req aigateway.ChatCompletionRequest) []byte {
	b, _ := json.Marshal(req)
	return b
}
func responseInputMessages(input interface{}) []aigateway.ChatMessage {
	if text, ok := input.(string); ok {
		return []aigateway.ChatMessage{{Role: "user", Content: text}}
	}
	items, ok := input.([]interface{})
	if !ok {
		return []aigateway.ChatMessage{{Role: "user", Content: protocolContentText(input)}}
	}
	result := make([]aigateway.ChatMessage, 0, len(items))
	for _, raw := range items {
		item, ok := raw.(map[string]interface{})
		if !ok {
			result = append(result, aigateway.ChatMessage{Role: "user", Content: protocolContentText(raw)})
			continue
		}
		kind, _ := item["type"].(string)
		role, _ := item["role"].(string)
		switch kind {
		case "function_call_output":
			result = append(result, aigateway.ChatMessage{Role: "tool", ToolCallID: fmt.Sprint(item["call_id"]), Content: item["output"]})
		case "function_call":
			result = append(result, aigateway.ChatMessage{Role: "assistant", ToolCalls: []aigateway.ToolCall{{ID: fmt.Sprint(item["call_id"]), Type: "function", Function: &aigateway.ToolCallFunction{Name: fmt.Sprint(item["name"]), Arguments: fmt.Sprint(item["arguments"])}}}})
		default:
			if role == "" {
				role = "user"
			}
			// Responses API requests may use the developer role for managed
			// instructions. OpenAI-compatible providers behind ClawManager
			// commonly only accept the Chat Completions system role.
			if role == "developer" {
				role = "system"
			}
			result = append(result, aigateway.ChatMessage{Role: role, Content: responseContent(item["content"])})
		}
	}
	return result
}

// responseContent preserves text and image input blocks when the selected
// provider is OpenAI-compatible. File/search/reasoning blocks have no common
// Chat Completions equivalent and are represented as text by protocolBlockText.
func responseContent(value interface{}) interface{} {
	blocks, ok := value.([]interface{})
	if !ok {
		return protocolBlockText(value)
	}
	out := make([]interface{}, 0, len(blocks))
	for _, raw := range blocks {
		block, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		switch block["type"] {
		case "input_text", "output_text", "text":
			if text, ok := block["text"].(string); ok {
				out = append(out, map[string]interface{}{"type": "text", "text": text})
			}
		case "input_image":
			url, _ := block["image_url"].(string)
			if url == "" {
				continue
			}
			image := map[string]interface{}{"url": url}
			if detail, ok := block["detail"].(string); ok && detail != "" {
				image["detail"] = detail
			}
			out = append(out, map[string]interface{}{"type": "image_url", "image_url": image})
		default:
			if text := protocolBlockText(block); text != "" {
				out = append(out, map[string]interface{}{"type": "text", "text": text})
			}
		}
	}
	if len(out) == 0 {
		return protocolBlockText(value)
	}
	return out
}
func anthropicMessages(role string, content interface{}) []aigateway.ChatMessage {
	blocks, ok := content.([]interface{})
	if !ok {
		return []aigateway.ChatMessage{{Role: role, Content: protocolBlockText(content)}}
	}
	message := aigateway.ChatMessage{Role: role}
	toolResults := make([]aigateway.ChatMessage, 0)
	var text []string
	for _, raw := range blocks {
		block, ok := raw.(map[string]interface{})
		if !ok {
			text = append(text, protocolContentText(raw))
			continue
		}
		switch block["type"] {
		case "tool_use":
			message.ToolCalls = append(message.ToolCalls, aigateway.ToolCall{ID: fmt.Sprint(block["id"]), Type: "function", Function: &aigateway.ToolCallFunction{Name: fmt.Sprint(block["name"]), Arguments: jsonValue(block["input"])}})
		case "tool_result":
			toolResults = append(toolResults, aigateway.ChatMessage{Role: "tool", ToolCallID: fmt.Sprint(block["tool_use_id"]), Content: protocolBlockText(block["content"])})
		default:
			text = append(text, protocolBlockText(block))
		}
	}
	message.Content = strings.Join(text, "\n")
	result := make([]aigateway.ChatMessage, 0, len(toolResults)+1)
	if message.Content != "" || len(message.ToolCalls) > 0 {
		result = append(result, message)
	}
	return append(result, toolResults...)
}
func jsonValue(v interface{}) string { b, _ := json.Marshal(v); return string(b) }
func responsesTools(raw json.RawMessage) json.RawMessage {
	var items []map[string]interface{}
	if json.Unmarshal(raw, &items) != nil {
		return raw
	}
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if item["type"] == "function" {
			out = append(out, map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": item["name"], "description": item["description"], "parameters": item["parameters"]}})
		}
	}
	b, _ := json.Marshal(out)
	return b
}
func anthropicTools(raw json.RawMessage) json.RawMessage {
	var items []map[string]interface{}
	if json.Unmarshal(raw, &items) != nil {
		return raw
	}
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": item["name"], "description": item["description"], "parameters": item["input_schema"]}})
	}
	b, _ := json.Marshal(out)
	return b
}
func anthropicToolChoice(raw json.RawMessage) json.RawMessage {
	var v map[string]interface{}
	if json.Unmarshal(raw, &v) != nil {
		return raw
	}
	if name, ok := v["name"]; ok {
		b, _ := json.Marshal(map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": name}})
		return b
	}
	return raw
}

func responseToResponses(id, model string, body []byte) gin.H {
	text, calls, usage := openAIResult(body)
	output := []interface{}{}
	if text != "" {
		output = append(output, gin.H{"type": "message", "role": "assistant", "content": []gin.H{{"type": "output_text", "text": text}}})
	}
	for _, call := range calls {
		name, arguments := toolCallParts(call)
		output = append(output, gin.H{"type": "function_call", "call_id": call.ID, "name": name, "arguments": arguments})
	}
	return gin.H{"id": id, "object": "response", "created_at": time.Now().Unix(), "status": "completed", "model": model, "output": output, "output_text": text, "usage": gin.H{"input_tokens": usage[0], "output_tokens": usage[1], "total_tokens": usage[2]}}
}
func responsesError(body []byte) gin.H {
	return gin.H{"type": "error", "error": gin.H{"type": "upstream_error", "message": upstreamErrorMessage(body)}}
}
func anthropicError(body []byte) gin.H {
	return gin.H{"type": "error", "error": gin.H{"type": "api_error", "message": upstreamErrorMessage(body)}}
}
func upstreamErrorMessage(body []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &payload) == nil {
		if strings.TrimSpace(payload.Error.Message) != "" {
			return payload.Error.Message
		}
		if strings.TrimSpace(payload.Message) != "" {
			return payload.Message
		}
	}
	if message := strings.TrimSpace(string(body)); message != "" {
		return message
	}
	return "upstream model provider request failed"
}
func responseToAnthropic(id, model string, body []byte) gin.H {
	text, calls, usage := openAIResult(body)
	content := []gin.H{}
	for _, call := range calls {
		name, arguments := toolCallParts(call)
		var input interface{}
		_ = json.Unmarshal([]byte(arguments), &input)
		if input == nil {
			input = map[string]interface{}{}
		}
		content = append(content, gin.H{"type": "tool_use", "id": call.ID, "name": name, "input": input})
	}
	if text != "" {
		content = append([]gin.H{{"type": "text", "text": text}}, content...)
	}
	stop := "end_turn"
	if len(calls) > 0 {
		stop = "tool_use"
	}
	return gin.H{"id": id, "type": "message", "role": "assistant", "model": model, "stop_reason": stop, "content": content, "usage": gin.H{"input_tokens": usage[0], "output_tokens": usage[1]}}
}
func toolCallParts(call aigateway.ToolCall) (string, string) {
	if call.Function == nil {
		return "", ""
	}
	return call.Function.Name, call.Function.Arguments
}
func openAIResult(body []byte) (string, []aigateway.ToolCall, [3]int) {
	var p struct {
		Choices []struct {
			Message struct {
				Content   interface{}          `json:"content"`
				ToolCalls []aigateway.ToolCall `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	_ = json.Unmarshal(body, &p)
	if len(p.Choices) == 0 {
		return "", nil, [3]int{}
	}
	return protocolContentText(p.Choices[0].Message.Content), p.Choices[0].Message.ToolCalls, [3]int{p.Usage.PromptTokens, p.Usage.CompletionTokens, p.Usage.TotalTokens}
}

// protocolStreamWriter receives normalized OpenAI chunks from the gateway and
// emits the protocol-specific SSE dialect without buffering the provider stream.
type protocolStreamWriter struct {
	http.ResponseWriter
	kind, model            string
	buffer                 bytes.Buffer
	created                bool
	block                  int
	calls                  map[int]aigateway.ToolCall
	toolStarted            map[int]bool
	textStarted            bool
	responseMessageStarted bool
	responseID             string
	responseCreatedAt      int64
	responseText           strings.Builder
	sequence               int
}

func (w *protocolStreamWriter) Write(p []byte) (int, error) {
	w.buffer.Write(p)
	for {
		data := w.buffer.Bytes()
		n := bytes.Index(data, []byte("\n\n"))
		if n < 0 {
			break
		}
		event := string(data[:n])
		w.buffer.Next(n + 2)
		w.translate(event)
	}
	return len(p), nil
}
func (w *protocolStreamWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (w *protocolStreamWriter) emit(event string, value interface{}) {
	if payload, ok := value.(gin.H); ok {
		w.sequence++
		payload["sequence_number"] = w.sequence
	}
	b, _ := json.Marshal(value)
	if event != "" {
		_, _ = fmt.Fprintf(w.ResponseWriter, "event: %s\n", event)
	}
	_, _ = fmt.Fprintf(w.ResponseWriter, "data: %s\n\n", b)
	w.Flush()
}
func (w *protocolStreamWriter) translate(raw string) {
	line := strings.TrimSpace(strings.TrimPrefix(raw, "data:"))
	if line == "[DONE]" {
		if w.kind == "anthropic" {
			w.emit("message_stop", gin.H{"type": "message_stop"})
		}
		return
	}
	var chunk struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			Delta struct {
				Content   interface{}          `json:"content"`
				ToolCalls []aigateway.ToolCall `json:"tool_calls"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal([]byte(line), &chunk) != nil {
		return
	}
	if w.model == "" {
		w.model = chunk.Model
	}
	if w.responseID == "" {
		w.responseID = chunk.ID
		if w.responseID == "" {
			w.responseID = "resp_" + fmt.Sprint(time.Now().UnixNano())
		}
		w.responseCreatedAt = time.Now().Unix()
	}
	if !w.created {
		w.created = true
		if w.kind == "responses" {
			w.emit("response.created", gin.H{"type": "response.created", "response": w.responsesSnapshot("in_progress", []interface{}{})})
		} else {
			w.emit("message_start", gin.H{"type": "message_start", "message": gin.H{"id": chunk.ID, "type": "message", "role": "assistant", "model": w.model, "content": []interface{}{}}})
		}
	}
	for _, choice := range chunk.Choices {
		if text := protocolContentText(choice.Delta.Content); text != "" {
			if w.kind == "responses" {
				if !w.responseMessageStarted {
					w.responseMessageStarted = true
					w.emit("response.output_item.added", gin.H{"type": "response.output_item.added", "output_index": 0, "item": w.responsesMessage("in_progress", "")})
					w.emit("response.content_part.added", gin.H{"type": "response.content_part.added", "item_id": w.responsesMessageID(), "output_index": 0, "content_index": 0, "part": gin.H{"type": "output_text", "text": "", "annotations": []interface{}{}}})
				}
				w.responseText.WriteString(text)
				w.emit("response.output_text.delta", gin.H{"type": "response.output_text.delta", "item_id": w.responsesMessageID(), "delta": text, "output_index": 0, "content_index": 0})
			} else {
				if !w.textStarted {
					w.emit("content_block_start", gin.H{"type": "content_block_start", "index": 0, "content_block": gin.H{"type": "text", "text": ""}})
					w.textStarted = true
				}
				w.emit("content_block_delta", gin.H{"type": "content_block_delta", "index": 0, "delta": gin.H{"type": "text_delta", "text": text}})
			}
		}
		for _, call := range choice.Delta.ToolCalls {
			if call.Index == nil {
				continue
			}
			idx := *call.Index
			if w.calls == nil {
				w.calls = map[int]aigateway.ToolCall{}
			}
			if w.toolStarted == nil {
				w.toolStarted = map[int]bool{}
			}
			old := w.calls[idx]
			if call.ID != "" {
				old.ID = call.ID
			}
			if call.Function != nil {
				if old.Function == nil {
					old.Function = &aigateway.ToolCallFunction{}
				}
				if call.Function.Name != "" {
					old.Function.Name = call.Function.Name
				}
				old.Function.Arguments += call.Function.Arguments
			}
			w.calls[idx] = old
			if w.kind == "responses" {
				if call.ID != "" {
					w.emit("response.output_item.added", gin.H{"type": "response.output_item.added", "output_index": idx + 1, "item": gin.H{"type": "function_call", "call_id": old.ID, "name": old.Function.Name, "arguments": ""}})
				}
				if call.Function != nil && call.Function.Arguments != "" {
					w.emit("response.function_call_arguments.delta", gin.H{"type": "response.function_call_arguments.delta", "output_index": idx + 1, "delta": call.Function.Arguments})
				}
			} else {
				if !w.toolStarted[idx] {
					w.toolStarted[idx] = true
					w.emit("content_block_start", gin.H{"type": "content_block_start", "index": idx + 1, "content_block": gin.H{"type": "tool_use", "id": old.ID, "name": old.Function.Name, "input": map[string]interface{}{}}})
				}
				if call.Function != nil && call.Function.Arguments != "" {
					w.emit("content_block_delta", gin.H{"type": "content_block_delta", "index": idx + 1, "delta": gin.H{"type": "input_json_delta", "partial_json": call.Function.Arguments}})
				}
			}
		}
		if choice.FinishReason != nil {
			if w.kind == "responses" {
				if w.responseMessageStarted {
					text := w.responseText.String()
					w.emit("response.output_text.done", gin.H{"type": "response.output_text.done", "item_id": w.responsesMessageID(), "text": text, "output_index": 0, "content_index": 0})
					w.emit("response.content_part.done", gin.H{"type": "response.content_part.done", "item_id": w.responsesMessageID(), "output_index": 0, "content_index": 0, "part": gin.H{"type": "output_text", "text": text, "annotations": []interface{}{}}})
					w.emit("response.output_item.done", gin.H{"type": "response.output_item.done", "output_index": 0, "item": w.responsesMessage("completed", text)})
				}
				for i, call := range w.calls {
					name, arguments := toolCallParts(call)
					w.emit("response.function_call_arguments.done", gin.H{"type": "response.function_call_arguments.done", "output_index": i + 1, "arguments": arguments})
					w.emit("response.output_item.done", gin.H{"type": "response.output_item.done", "output_index": i + 1, "item": gin.H{"type": "function_call", "call_id": call.ID, "name": name, "arguments": arguments}})
				}
				output := []interface{}{}
				if w.responseMessageStarted {
					output = append(output, w.responsesMessage("completed", w.responseText.String()))
				}
				w.emit("response.completed", gin.H{"type": "response.completed", "response": w.responsesSnapshot("completed", output)})
			} else {
				if w.textStarted {
					w.emit("content_block_stop", gin.H{"type": "content_block_stop", "index": 0})
				}
				for i := range w.toolStarted {
					w.emit("content_block_stop", gin.H{"type": "content_block_stop", "index": i + 1})
				}
				stop := "end_turn"
				if len(w.calls) > 0 {
					stop = "tool_use"
				}
				w.emit("message_delta", gin.H{"type": "message_delta", "delta": gin.H{"stop_reason": stop}, "usage": gin.H{"input_tokens": 0, "output_tokens": func() int {
					if chunk.Usage != nil {
						return chunk.Usage.CompletionTokens
					}
					return 0
				}()}})
			}
		}
	}
}

func (w *protocolStreamWriter) responsesMessageID() string { return "msg_" + w.responseID }

func (w *protocolStreamWriter) responsesMessage(status, text string) gin.H {
	content := []interface{}{}
	if status == "completed" {
		content = append(content, gin.H{"type": "output_text", "text": text, "annotations": []interface{}{}})
	}
	return gin.H{"id": w.responsesMessageID(), "type": "message", "status": status, "role": "assistant", "content": content}
}

func (w *protocolStreamWriter) responsesSnapshot(status string, output []interface{}) gin.H {
	return gin.H{"id": w.responseID, "object": "response", "created_at": w.responseCreatedAt, "status": status, "error": nil, "incomplete_details": nil, "instructions": nil, "max_output_tokens": nil, "model": w.model, "output": output, "parallel_tool_calls": true, "tool_choice": "auto", "tools": []interface{}{}, "top_p": 1, "temperature": 1, "truncation": "disabled", "usage": nil, "user": nil, "metadata": gin.H{}}
}

func applyGatewayRequestContext(c *gin.Context, req *aigateway.ChatCompletionRequest) bool {
	if req == nil {
		return false
	}
	if id, ok := c.Get("instanceID"); ok {
		if value, ok := id.(int); ok {
			req.InstanceID = &value
		}
	}
	if !setStringMetadata(c, &req.InstanceMode, "instanceMode") || !setStringMetadata(c, &req.RuntimeType, "runtimeType") || !setStringMetadata(c, &req.GatewayID, "gatewayID") || !setInt64Metadata(c, &req.RuntimePodID, "runtimePodID") {
		utils.Error(c, http.StatusForbidden, "Gateway token metadata does not match request")
		return false
	}
	authType, _ := c.Get("gatewayAuthType")
	instanceType, _ := c.Get("instanceType")
	aigateway.ApplyManagedInstanceSessionDefaults(req, stringValue(authType), stringValue(instanceType))
	return true
}
func gatewayUserID(c *gin.Context) int { value, _ := c.Get("userID"); id, _ := value.(int); return id }
func protocolContentText(value interface{}) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	raw, _ := json.Marshal(value)
	return string(raw)
}

// protocolBlockText extracts text from Responses and Anthropic content blocks
// instead of serializing their wire representation into the model prompt.
func protocolBlockText(value interface{}) string {
	if text, ok := value.(string); ok {
		return text
	}
	blocks, ok := value.([]interface{})
	if !ok {
		if block, ok := value.(map[string]interface{}); ok {
			return protocolBlockText([]interface{}{block})
		}
		return protocolContentText(value)
	}
	parts := make([]string, 0, len(blocks))
	for _, raw := range blocks {
		block, ok := raw.(map[string]interface{})
		if !ok {
			parts = append(parts, protocolContentText(raw))
			continue
		}
		typ, _ := block["type"].(string)
		switch typ {
		case "input_text", "output_text", "text":
			if text, ok := block["text"].(string); ok && text != "" {
				parts = append(parts, text)
			}
		default:
			if text, ok := block["text"].(string); ok && text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}
