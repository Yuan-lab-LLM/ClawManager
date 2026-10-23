package aigateway

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net"
	"net/url"
	"strings"

	"clawreef/internal/models"
	_ "golang.org/x/image/webp"
)

// Deliberate gateway limits, not claims about every upstream model's limits.
const MaxVisionRequestBytes int64 = 32 << 20
const maxVisionImages = 16
const maxVisionImageBytes = 5 << 20

// A conservative reservation, not a provider tokenizer. Actual usage wins.
const visionImageTokenReservation = 8192

func invalidImage(reason string) error {
	return fmt.Errorf("%w: %s", models.ErrInvalidImageInput, reason)
}

func isImageBlock(part map[string]interface{}) bool {
	switch part["type"] {
	case "image_url", "input_image", "image", "image_base64":
		return true
	}
	return false
}

// NormalizeVisionContent converts image blocks without fetching remote URLs.
// Opaque provider file IDs must wait for a governed, credential-bound resolver.
func NormalizeVisionContent(value interface{}) (interface{}, error) {
	count := 0
	return normalizeVisionContent(value, &count, 0)
}

func normalizeVisionContent(value interface{}, count *int, depth int) (interface{}, error) {
	if depth > 32 {
		return nil, invalidImage("content nesting limit exceeded")
	}
	switch v := value.(type) {
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, item := range v {
			normalized, err := normalizeVisionContent(item, count, depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = normalized
		}
		return out, nil
	case map[string]interface{}:
		if isImageBlock(v) {
			*count++
			if *count > maxVisionImages {
				return nil, invalidImage("at most 16 images per request")
			}
			return normalizeImageBlock(v)
		}
		out := make(map[string]interface{}, len(v))
		for key, item := range v {
			out[key] = item
			// Never interpret quoted text or tool/function arguments as images.
			if key == "content" || key == "input" || key == "messages" {
				normalized, err := normalizeVisionContent(item, count, depth+1)
				if err != nil {
					return nil, err
				}
				out[key] = normalized
			}
		}
		return out, nil
	}
	return value, nil
}

func normalizeImageBlock(block map[string]interface{}) (interface{}, error) {
	var reference, detail string
	if block["file_id"] != nil || block["transformations"] != nil {
		return nil, invalidImage("file references and image transformations are not supported by this gateway yet")
	}
	switch block["type"] {
	case "input_image":
		reference, _ = block["image_url"].(string)
		if value, exists := block["detail"]; exists {
			if _, ok := value.(string); !ok {
				return nil, invalidImage("image detail must be a string")
			}
		}
		detail, _ = block["detail"].(string)
	case "image_url":
		img, ok := block["image_url"].(map[string]interface{})
		if !ok {
			return nil, invalidImage("image_url must contain a url object")
		}
		reference, _ = img["url"].(string)
		if value, exists := img["detail"]; exists {
			if _, ok := value.(string); !ok {
				return nil, invalidImage("image detail must be a string")
			}
		}
		detail, _ = img["detail"].(string)
	case "image":
		source, ok := block["source"].(map[string]interface{})
		if !ok {
			return nil, invalidImage("image source is required")
		}
		switch source["type"] {
		case "url":
			reference, _ = source["url"].(string)
		case "base64":
			mediaType, _ := source["media_type"].(string)
			data, _ := source["data"].(string)
			reference = "data:" + mediaType + ";base64," + data
		default:
			return nil, invalidImage("unsupported image source; use URL or base64")
		}
	default:
		return nil, invalidImage("unsupported image block type")
	}
	if detail != "" && detail != "auto" && detail != "low" && detail != "high" {
		return nil, invalidImage("unsupported image detail")
	}
	if err := validateImageReference(reference); err != nil {
		return nil, err
	}
	img := map[string]interface{}{"url": reference}
	if detail != "" {
		img["detail"] = detail
	}
	return map[string]interface{}{"type": "image_url", "image_url": img}, nil
}

func validateImageReference(reference string) error {
	if strings.HasPrefix(reference, "data:") {
		header, data, ok := strings.Cut(reference, ",")
		if !ok || len(data) > base64.StdEncoding.EncodedLen(maxVisionImageBytes) {
			return invalidImage("invalid or oversized base64 image (maximum 5 MiB decoded)")
		}
		formats := map[string]string{"data:image/png;base64": "png", "data:image/jpeg;base64": "jpeg", "data:image/gif;base64": "gif", "data:image/webp;base64": "webp"}
		format, supported := formats[header]
		if !supported {
			return invalidImage("inline images support PNG, JPEG, GIF and WebP")
		}
		raw, err := base64.StdEncoding.Strict().DecodeString(data)
		if err != nil || len(raw) == 0 || len(raw) > maxVisionImageBytes {
			return invalidImage("invalid or oversized base64 image")
		}
		config, actual, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil || actual != format || config.Width <= 0 || config.Height <= 0 || config.Width > 8192 || config.Height > 8192 || int64(config.Width)*int64(config.Height) > 32_000_000 {
			return invalidImage("invalid image format or excessive dimensions")
		}
		return nil
	}
	u, err := url.Parse(reference)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(reference) > 8192 {
		return invalidImage("image URL must be an absolute HTTPS URL without credentials or fragment")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || !strings.Contains(host, ".") {
		return invalidImage("private image URLs are not supported")
	}
	if ip := net.ParseIP(host); ip != nil && (!ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()) {
		return invalidImage("private image URLs are not supported")
	}
	return nil
}

// NormalizeVisionRequest is called before rate limiting and again for internal
// callers. RawBody is synchronized so proxy extensions cannot undo conversion.
func NormalizeVisionRequest(req *ChatCompletionRequest) error {
	if req == nil {
		return nil
	}
	count := 0
	messages := append([]ChatMessage(nil), req.Messages...)
	for i := range messages {
		content, err := normalizeVisionContent(messages[i].Content, &count, 0)
		if err != nil {
			return err
		}
		if HasImageContent(content) && !flatVisionContent(content) {
			return invalidImage("image content must be a flat array of text and image blocks")
		}
		messages[i].Content = content
	}
	if count == 0 {
		return nil
	}
	req.Messages = messages
	if len(req.RawBody) > 0 {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(req.RawBody, &raw); err != nil {
			return invalidImage("invalid request JSON")
		}
		// Preserve provider extensions such as reasoning_content on assistant
		// tool-call messages. Only image content is changed by normalization.
		var original []map[string]json.RawMessage
		if err := json.Unmarshal(raw["messages"], &original); err == nil && len(original) == len(messages) {
			for i := range original {
				if original[i] == nil {
					return invalidImage("invalid message object")
				}
				original[i]["content"], _ = json.Marshal(messages[i].Content)
			}
			raw["messages"], _ = json.Marshal(original)
		} else {
			raw["messages"], _ = json.Marshal(messages)
		}
		req.RawBody, _ = json.Marshal(raw)
	}
	return nil
}

func imageCount(value interface{}) int {
	switch v := value.(type) {
	case []interface{}:
		n := 0
		for _, item := range v {
			n += imageCount(item)
		}
		return n
	case map[string]interface{}:
		if isImageBlock(v) {
			return 1
		}
		return imageCount(v["content"])
	}
	return 0
}

func HasImageContent(value interface{}) bool { return imageCount(value) > 0 }

// At this boundary protocol-specific tool_result wrappers have already been
// converted into tool-role messages. Reject other nested wrappers instead of
// flattening their images and changing their meaning.
func flatVisionContent(value interface{}) bool {
	items, ok := value.([]interface{})
	if !ok {
		return false
	}
	for _, item := range items {
		if !HasImageContent(item) {
			continue
		}
		part, ok := item.(map[string]interface{})
		if !ok || part["type"] != "image_url" {
			return false
		}
	}
	return true
}

func visionTransportSupported(protocol string, req ChatCompletionRequest) bool {
	for _, message := range req.Messages {
		if !HasImageContent(message.Content) {
			continue
		}
		if message.Role != "user" && !(message.Role == "tool" && protocol == models.ProtocolTypeAnthropic) {
			return false
		}
		if protocol == models.ProtocolTypeAnthropic && hasExplicitImageDetail(message.Content) {
			return false
		}
	}
	return true
}

func hasExplicitImageDetail(value interface{}) bool {
	switch v := value.(type) {
	case []interface{}:
		for _, item := range v {
			if hasExplicitImageDetail(item) {
				return true
			}
		}
	case map[string]interface{}:
		if v["type"] == "image_url" {
			img, _ := v["image_url"].(map[string]interface{})
			detail, _ := img["detail"].(string)
			return detail != "" && detail != "auto"
		}
		return hasExplicitImageDetail(v["content"])
	}
	return false
}

func requestRequiresVision(req ChatCompletionRequest) bool {
	for _, message := range req.Messages {
		if imageCount(message.Content) > 0 {
			return true
		}
	}
	return false
}

// This copy is for accounting/logging ONLY; never forward it to a provider.
func redactImageContent(value interface{}) (interface{}, int) {
	switch v := value.(type) {
	case []interface{}:
		out := make([]interface{}, len(v))
		n := 0
		for i, item := range v {
			clean, count := redactImageContent(item)
			out[i] = clean
			n += count
		}
		return out, n
	case map[string]interface{}:
		if isImageBlock(v) {
			return map[string]interface{}{"type": "text", "text": "[image omitted]"}, 1
		}
		out := make(map[string]interface{}, len(v))
		n := 0
		for key, item := range v {
			clean, count := redactImageContent(item)
			out[key] = clean
			n += count
		}
		return out, n
	}
	return value, 0
}

func redactedVisionJSON(raw []byte) ([]byte, int) {
	var value interface{}
	if json.Unmarshal(raw, &value) != nil {
		return []byte("[invalid JSON omitted]"), 0
	}
	clean, count := redactImageContent(value)
	if count == 0 {
		return raw, 0
	}
	result, _ := json.Marshal(clean)
	return result, count
}

func visionSafePayload(raw []byte) string { clean, _ := redactedVisionJSON(raw); return string(clean) }

func visionAccountingTokens(raw []byte) int {
	if len(raw) == 0 {
		return 0
	}
	clean, count := redactedVisionJSON(raw)
	return estimateGatewayInputTokens(string(clean)) + count*visionImageTokenReservation
}

func visionAnthropicBlocks(content interface{}) ([]anthropicContentBlock, bool) {
	if imageCount(content) == 0 {
		return nil, false
	}
	items, ok := content.([]interface{})
	if !ok {
		items = []interface{}{content}
	}
	blocks := make([]anthropicContentBlock, 0, len(items))
	for _, item := range items {
		if part, ok := item.(map[string]interface{}); ok && isImageBlock(part) && part["type"] != "image_url" {
			normalized, err := NormalizeVisionContent(part)
			if err != nil {
				return nil, true
			} // Public execution validates before conversion.
			item = normalized
		}
		part, _ := item.(map[string]interface{})
		if part["type"] == "image_url" {
			img, _ := part["image_url"].(map[string]interface{})
			ref, _ := img["url"].(string)
			source := map[string]interface{}{"type": "url", "url": ref}
			if strings.HasPrefix(ref, "data:") {
				header, data, _ := strings.Cut(ref, ",")
				source = map[string]interface{}{"type": "base64", "media_type": strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64"), "data": data}
			}
			blocks = append(blocks, anthropicContentBlock{Type: "image", Source: source})
		} else if !HasImageContent(item) {
			blocks = append(blocks, convertContentToAnthropicTextBlocks(item)...)
		} else {
			// Defensive guard for internal callers; public requests reject nested
			// image wrappers in NormalizeVisionRequest before this conversion.
			return nil, true
		}
	}
	return blocks, true
}
