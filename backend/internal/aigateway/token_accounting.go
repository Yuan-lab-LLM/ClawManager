package aigateway

import (
	"encoding/json"
	"errors"
	"unicode/utf8"
)

// One default is both reserved and sent upstream, including Anthropic.
const defaultGatewayOutputTokens = 4096

// NormalizeGenerationLimits runs before TPM acquisition and again for internal
// callers. RawBody aliases are read too, so accounting and the wire cannot differ.
func NormalizeGenerationLimits(req *ChatCompletionRequest) error {
	if req == nil {
		return nil
	}
	var wire struct {
		MaxTokens           *int `json:"max_tokens"`
		MaxCompletionTokens *int `json:"max_completion_tokens"`
		N                   *int `json:"n"`
	}
	if len(req.RawBody) > 0 {
		if err := json.Unmarshal(req.RawBody, &wire); err != nil {
			return errors.New("invalid generation limits or request body")
		}
	}
	// Preserve the existing max_tokens precedence when clients send both aliases.
	// Whichever alias wins is also written to the wire, never just estimated.
	limits := []*int{req.MaxTokens, wire.MaxTokens, req.MaxCompletionTokens, wire.MaxCompletionTokens}
	var output *int
	for _, limit := range limits {
		if limit == nil {
			continue
		}
		if *limit <= 0 || int64(*limit) > 2147483647 {
			return errors.New("output token limit must be between 1 and 2147483647")
		}
		if output == nil {
			output = limit
		}
	}
	if output == nil {
		value := defaultGatewayOutputTokens
		output = &value
	}
	if req.N != nil && wire.N != nil && *req.N != *wire.N {
		return errors.New("conflicting completion counts")
	}
	if req.N == nil {
		req.N = wire.N
	}
	if req.N != nil && (*req.N <= 0 || int64(*req.N) > 2147483647 || int64(*req.N)*int64(*output) > 2147483647) {
		return errors.New("invalid completion count")
	}
	value := *output
	req.MaxTokens = &value
	if wire.MaxTokens != nil {
		req.MaxCompletionTokens = nil
	} else if req.MaxCompletionTokens == nil {
		req.MaxCompletionTokens = wire.MaxCompletionTokens
	}
	return nil
}

func routingTokenEstimate(req ChatCompletionRequest) (int, int) {
	// Requests are validated before selection. Keep a conservative fallback for
	// standalone estimation callers; no provider-specific tokenizer is implied.
	_ = NormalizeGenerationLimits(&req)
	payload, _ := json.Marshal(struct {
		Messages       []ChatMessage   `json:"messages"`
		Tools          json.RawMessage `json:"tools,omitempty"`
		ResponseFormat json.RawMessage `json:"response_format,omitempty"`
	}{req.Messages, req.Tools, req.ResponseFormat})
	prompt := visionAccountingTokens(payload)
	// Unknown extensions and raw-only internal callers must not lower reservation.
	if rawEstimate := visionAccountingTokens(req.RawBody); rawEstimate > prompt {
		prompt = rawEstimate
	}
	output := defaultGatewayOutputTokens
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		output = *req.MaxTokens
	}
	if req.N != nil && *req.N > 1 && int64(*req.N) <= 2147483647 && int64(output)*int64(*req.N) <= 2147483647 {
		output *= *req.N
	}
	return prompt, output
}

func estimateGatewayInputTokens(text string) int {
	ascii, other := 0, 0
	for _, r := range text {
		if r < utf8.RuneSelf {
			ascii++
		} else {
			other += (utf8.RuneLen(r) + 1) / 2
		}
	}
	return (ascii+3)/4 + other
}

func resolveRequestUsage(req ChatCompletionRequest, response string, prompt, completion, total int) (int, int, int, bool) {
	prompt, completion, total, estimated := resolveUsage(req.Messages, response, prompt, completion, total)
	if estimated {
		prompt, _ = routingTokenEstimate(req)
		completion = estimateGatewayInputTokens(response)
		total = prompt + completion
	}
	return prompt, completion, total, estimated
}
