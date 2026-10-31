package aigateway

// Images API wire adapter. This is deliberately NOT a public execution path:
// handlers may expose it only after operation-specific authorization, price
// reservation, cancellation and result storage are connected. Image input
// support does not grant permission or capability to generate images.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"strconv"
	"strings"
	"unicode/utf8"

	"clawreef/internal/models"
)

type ImageOperation string

const (
	ImageGenerate ImageOperation = "generate"
	ImageEdit     ImageOperation = "edit"
)

var ErrInvalidImageOperation = errors.New("invalid image operation")

type imageUpload struct {
	field         string
	format        string
	data          []byte
	width, height int
}

type ImageOperationRequest struct {
	operation     ImageOperation
	fields        map[string]json.RawMessage
	uploads       []imageUpload
	model, prompt string
	count         int
	streaming     bool
}

func (r *ImageOperationRequest) Model() string   { return r.model }
func (r *ImageOperationRequest) Prompt() string  { return r.prompt }
func (r *ImageOperationRequest) Count() int      { return r.count }
func (r *ImageOperationRequest) Streaming() bool { return r.streaming }

// ParseImageOperationRequest never fetches URLs or writes uploaded files to disk.
// Limits are Gateway policy, not a claim about every provider's maximum limits.
func ParseImageOperationRequest(op ImageOperation, contentType string, body io.Reader) (*ImageOperationRequest, error) {
	fail := func(reason string) (*ImageOperationRequest, error) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidImageOperation, reason)
	}
	if op != ImageGenerate && op != ImageEdit {
		return fail("unsupported operation")
	}
	if body == nil {
		return fail("request body is required")
	}
	raw, err := io.ReadAll(io.LimitReader(body, MaxVisionRequestBytes+1))
	if err != nil {
		return fail("unable to read request")
	}
	if int64(len(raw)) > MaxVisionRequestBytes {
		return fail("request exceeds 32 MiB")
	}
	media, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return fail("invalid content type")
	}
	request := &ImageOperationRequest{operation: op, fields: map[string]json.RawMessage{}, count: 1}
	switch media {
	case "application/json":
		if err := json.Unmarshal(raw, &request.fields); err != nil || request.fields == nil {
			return fail("body must be a JSON object")
		}
	case "multipart/form-data":
		if op != ImageEdit || params["boundary"] == "" {
			return fail("multipart is supported only for image edits")
		}
		reader := multipart.NewReader(bytes.NewReader(raw), params["boundary"])
		parts := 0
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fail("invalid multipart body")
			}
			parts++
			if parts > 40 {
				return fail("too many multipart parts")
			}
			name := part.FormName()
			value, err := io.ReadAll(io.LimitReader(part, maxVisionImageBytes+1))
			_ = part.Close()
			if err != nil || len(value) > maxVisionImageBytes {
				return fail("invalid or oversized multipart part")
			}
			if part.FileName() != "" {
				if name != "image" && name != "image[]" && name != "mask" {
					return fail("unsupported uploaded file field")
				}
				config, format, err := image.DecodeConfig(bytes.NewReader(value))
				if err != nil || (format != "png" && format != "jpeg" && format != "webp") {
					return fail("edit uploads require PNG, JPEG or WebP")
				}
				if err := validateImageReference("data:image/" + format + ";base64," + base64.StdEncoding.EncodeToString(value)); err != nil {
					return fail("invalid image upload")
				}
				if name == "mask" && format != "png" {
					return fail("mask must be PNG")
				}
				request.uploads = append(request.uploads, imageUpload{field: name, format: format, data: value, width: config.Width, height: config.Height})
			} else {
				if _, exists := request.fields[name]; exists {
					return fail("duplicate form field")
				}
				if len(value) > 128000 {
					return fail("form field too large")
				}
				switch name {
				case "n", "output_compression", "partial_images":
					number, err := strconv.Atoi(string(value))
					if err != nil {
						return fail("numeric field must be an integer")
					}
					request.fields[name], _ = json.Marshal(number)
				case "stream":
					if string(value) != "true" && string(value) != "false" {
						return fail("stream must be boolean")
					}
					request.fields[name] = append([]byte(nil), value...)
				default:
					request.fields[name], _ = json.Marshal(string(value))
				}
			}
		}
	default:
		return fail("use application/json or multipart/form-data")
	}
	if err := request.validate(); err != nil {
		return nil, err
	}
	return request, nil
}

func (r *ImageOperationRequest) validate() error {
	fail := func(reason string) error { return fmt.Errorf("%w: %s", ErrInvalidImageOperation, reason) }
	allowed := map[string]bool{"model": true, "prompt": true, "n": true, "size": true, "quality": true, "background": true, "output_format": true, "output_compression": true, "response_format": true, "moderation": true, "style": true, "user": true, "safety_identifier": true, "stream": true, "partial_images": true}
	if r.operation == ImageEdit {
		allowed["images"] = true
		allowed["mask"] = true
		allowed["input_fidelity"] = true
	}
	for key := range r.fields {
		if !allowed[key] {
			return fail("unsupported request field: " + key)
		}
	}
	if err := json.Unmarshal(r.fields["model"], &r.model); err != nil || strings.TrimSpace(r.model) == "" || len(r.model) > 256 {
		return fail("model is required")
	}
	if err := json.Unmarshal(r.fields["prompt"], &r.prompt); err != nil || strings.TrimSpace(r.prompt) == "" || utf8.RuneCountInString(r.prompt) > 32000 {
		return fail("prompt must contain 1 to 32000 characters")
	}
	for _, field := range []struct {
		name     string
		target   *int
		min, max int
	}{{"n", &r.count, 1, 10}, {"output_compression", new(int), 0, 100}, {"partial_images", new(int), 0, 3}} {
		if raw, ok := r.fields[field.name]; ok && string(raw) != "null" {
			if err := json.Unmarshal(raw, field.target); err != nil || *field.target < field.min || *field.target > field.max {
				return fail("invalid " + field.name)
			}
		}
	}
	if raw, ok := r.fields["stream"]; ok {
		if err := json.Unmarshal(raw, &r.streaming); err != nil {
			return fail("stream must be boolean")
		}
	}
	if raw, ok := r.fields["partial_images"]; ok && string(raw) != "null" && !r.streaming {
		return fail("partial_images requires stream=true")
	}
	for key, raw := range r.fields {
		switch key {
		case "n", "output_compression", "partial_images", "stream", "images", "mask":
			continue
		}
		if string(raw) == "null" {
			continue
		}
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return fail(key + " must be a string")
		}
		if key == "safety_identifier" && utf8.RuneCountInString(s) > 128 {
			return fail("safety_identifier must be at most 128 characters")
		}
	}
	if r.operation == ImageEdit {
		count := 0
		var first, mask *imageUpload
		for i := range r.uploads {
			item := &r.uploads[i]
			if item.field == "mask" {
				if mask != nil {
					return fail("only one mask is allowed")
				}
				mask = item
			} else {
				count++
				if first == nil {
					first = item
				}
			}
		}
		if len(r.uploads) > 0 {
			if _, ok := r.fields["images"]; ok {
				return fail("cannot mix uploaded and referenced images")
			}
			if _, ok := r.fields["mask"]; ok {
				return fail("mask must be uploaded with multipart edits")
			}
			if mask != nil && first != nil && (mask.width != first.width || mask.height != first.height) {
				return fail("mask dimensions must match the first image")
			}
		} else {
			var images []map[string]json.RawMessage
			if err := json.Unmarshal(r.fields["images"], &images); err != nil {
				return fail("images must be a reference array")
			}
			count = len(images)
			for _, reference := range images {
				if err := validateEditReference(reference); err != nil {
					return err
				}
			}
			if raw, ok := r.fields["mask"]; ok && string(raw) != "null" {
				var reference map[string]json.RawMessage
				if json.Unmarshal(raw, &reference) != nil {
					return fail("mask must be an image reference")
				}
				if err := validateEditReference(reference); err != nil {
					return err
				}
			}
		}
		if count < 1 || count > maxVisionImages {
			return fail("edits require 1 to 16 images")
		}
	}
	return nil
}

func validateEditReference(reference map[string]json.RawMessage) error {
	if len(reference) != 1 || reference["image_url"] == nil {
		return fmt.Errorf("%w: use image_url; file IDs require an authorized resolver", ErrInvalidImageOperation)
	}
	var url string
	if json.Unmarshal(reference["image_url"], &url) != nil {
		return fmt.Errorf("%w: invalid image URL", ErrInvalidImageOperation)
	}
	if err := validateImageReference(url); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidImageOperation, err)
	}
	return nil
}

// ProviderBody replaces only the public alias. It does not execute a request,
// select a deployment, supply credentials, or bypass Gateway governance.
func (r *ImageOperationRequest) ProviderBody(model *models.LLMModel) (body []byte, contentType string, err error) {
	if err := r.ValidateModel(model); err != nil {
		return nil, "", err
	}
	if model == nil || strings.TrimSpace(model.ProviderModelName) == "" {
		return nil, "", fmt.Errorf("%w: missing provider model", ErrInvalidImageOperation)
	}
	protocol := models.ResolveLLMProtocolTypeOrDefault(model.ProviderType, model.ProtocolType)
	if protocol != models.ProtocolTypeOpenAI && protocol != models.ProtocolTypeOpenAICompatible {
		return nil, "", fmt.Errorf("%w: native image operations require OpenAI image protocol", ErrInvalidImageOperation)
	}
	fields := make(map[string]json.RawMessage, len(r.fields))
	for key, value := range r.fields {
		fields[key] = value
	}
	fields["model"], _ = json.Marshal(model.ProviderModelName)
	// Normalize omitted/null n to the count used for governance reservation.
	fields["n"], _ = json.Marshal(r.count)
	if len(r.uploads) == 0 {
		raw, err := json.Marshal(fields)
		return raw, "application/json", err
	}
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for key, value := range fields {
		var text string
		if string(value) == "null" {
			continue
		}
		if json.Unmarshal(value, &text) != nil {
			text = string(value)
		}
		if err := writer.WriteField(key, text); err != nil {
			return nil, "", err
		}
	}
	imageCount := 0
	for _, file := range r.uploads {
		if file.field != "mask" {
			imageCount++
		}
	}
	for i, file := range r.uploads {
		field := file.field
		if field != "mask" && imageCount > 1 {
			field = "image[]"
		}
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": field, "filename": fmt.Sprintf("image-%d.%s", i, file.format)}))
		header.Set("Content-Type", "image/"+file.format)
		part, err := writer.CreatePart(header)
		if err != nil {
			return nil, "", err
		}
		if _, err = part.Write(file.data); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return buffer.Bytes(), writer.FormDataContentType(), nil
}

// AuditMetadata contains no prompt, signed URLs, filenames or binary data.
func (r *ImageOperationRequest) AuditMetadata() map[string]interface{} {
	images := 0
	mask := false
	for _, upload := range r.uploads {
		if upload.field == "mask" {
			mask = true
		} else {
			images++
		}
	}
	if raw := r.fields["images"]; raw != nil {
		var refs []json.RawMessage
		_ = json.Unmarshal(raw, &refs)
		images = len(refs)
	}
	if raw := r.fields["mask"]; raw != nil && string(raw) != "null" {
		mask = true
	}
	return map[string]interface{}{"operation": r.operation, "requested_model": r.model, "output_count": r.count, "input_images": images, "has_mask": mask, "stream": r.streaming}
}
