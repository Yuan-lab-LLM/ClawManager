package aigateway

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"testing"

	"clawreef/internal/models"
)

func TestImageGenerateAdapterPreservesOptionsAndReplacesAlias(t *testing.T) {
	r, err := ParseImageOperationRequest(ImageGenerate, "application/json", strings.NewReader(`{"model":"public-alias","prompt":"画一只猫","n":2,"size":"1536x1024","quality":"high","background":"transparent","output_format":"webp","stream":true,"partial_images":2}`))
	if err != nil {
		t.Fatal(err)
	}
	body, contentType, err := r.ProviderBody(&models.LLMModel{ModelKind: models.ModelKindImage, ImageGenerationEnabled: true, ProviderType: models.ProviderTypeLocal, ProtocolType: models.ProtocolTypeOpenAICompatible, ProviderModelName: "image-upstream"})
	if err != nil || contentType != "application/json" {
		t.Fatal(err, contentType)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got["model"] != "image-upstream" || got["prompt"] != "画一只猫" || got["quality"] != "high" || got["partial_images"] != float64(2) || r.Model() != "public-alias" || r.Count() != 2 || !r.Streaming() {
		t.Fatal(got)
	}
	if _, _, err := r.ProviderBody(&models.LLMModel{ProviderType: models.ProviderTypeAnthropic, ProviderModelName: "claude"}); !errors.Is(err, ErrInvalidImageOperation) {
		t.Fatal("Anthropic must not be misrepresented as a native image generator")
	}
}

func TestImageSafetyIdentifierValidation(t *testing.T) {
	for _, size := range []int{128, 129} {
		raw := `{"model":"image","prompt":"draw","safety_identifier":"` + strings.Repeat("x", size) + `"}`
		_, err := ParseImageOperationRequest(ImageGenerate, "application/json", strings.NewReader(raw))
		if (err == nil) != (size == 128) {
			t.Fatal(size, err)
		}
	}
}

func TestImageEditJSONReferencesAndAudit(t *testing.T) {
	data := visionTestPNG(t)
	raw := `{"model":"edit","prompt":"private prompt","images":[{"image_url":"data:image/png;base64,` + data + `"}],"mask":{"image_url":"https://example.com/mask.png?token=secret"},"n":1}`
	r, err := ParseImageOperationRequest(ImageEdit, "application/json", strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	body, _, err := r.ProviderBody(&models.LLMModel{ModelKind: models.ModelKindImage, ImageEditEnabled: true, ProviderType: models.ProviderTypeOpenAI, ProviderModelName: "gpt-image-test"})
	if err != nil || !strings.Contains(string(body), data) {
		t.Fatal("image bytes lost", err)
	}
	metadata, _ := json.Marshal(r.AuditMetadata())
	for _, secret := range []string{data, "private prompt", "secret", "example.com"} {
		if strings.Contains(string(metadata), secret) {
			t.Fatalf("image operation audit leaked %q", secret)
		}
	}
	if r.AuditMetadata()["input_images"] != 1 || r.AuditMetadata()["has_mask"] != true {
		t.Fatal(r.AuditMetadata())
	}
}

func editMultipart(t *testing.T, fields map[string][]string, uploads [][2]string) ([]byte, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for key, values := range fields {
		for _, value := range values {
			if err := w.WriteField(key, value); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, upload := range uploads {
		part, err := w.CreateFormFile(upload[0], "untrusted-name.png")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(part, upload[1])
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes(), w.FormDataContentType()
}

func TestImageEditMultipartPreservesBytesAndSanitizesFilename(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString(visionTestPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	raw, contentType := editMultipart(t, map[string][]string{"model": {"edit"}, "prompt": {"change color"}, "n": {"2"}}, [][2]string{{"image[]", string(png)}, {"image[]", string(png)}, {"mask", string(png)}})
	r, err := ParseImageOperationRequest(ImageEdit, contentType, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	body, contentType, err := r.ProviderBody(&models.LLMModel{ModelKind: models.ModelKindImage, ImageEditEnabled: true, ProviderType: models.ProviderTypeOpenAICompatible, ProviderModelName: "physical"})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte("untrusted-name")) {
		t.Fatal("untrusted filename retained")
	}
	_, params, _ := mime.ParseMediaType(contentType)
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	count := 0
	mask := 0
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		content, _ := io.ReadAll(part)
		if part.FileName() != "" {
			if !bytes.Equal(content, png) {
				t.Fatal("changed image content")
			}
			if part.FormName() == "mask" {
				mask++
			} else {
				count++
			}
		}
		if part.FormName() == "model" && string(content) != "physical" {
			t.Fatal("alias not replaced")
		}
	}
	if count != 2 || mask != 1 || r.Count() != 2 {
		t.Fatal(count, mask, r.Count())
	}
}

func TestImageOperationRejectsAmbiguousAndUnsupportedInput(t *testing.T) {
	for _, raw := range []string{
		`null`, `[]`, `{"model":"m","prompt":""}`, `{"model":"m","prompt":"x","n":0}`, `{"model":"m","prompt":"x","n":11}`,
		`{"model":"m","prompt":"x","n":1.5}`, `{"model":"m","prompt":"x","stream":"true"}`, `{"model":"m","prompt":"x","partial_images":1}`,
		`{"model":"m","prompt":"x","output_compression":101}`, `{"model":"m","prompt":"x","unknown_field":1}`,
	} {
		if _, err := ParseImageOperationRequest(ImageGenerate, "application/json", strings.NewReader(raw)); !errors.Is(err, ErrInvalidImageOperation) {
			t.Fatalf("accepted %s: %v", raw, err)
		}
	}
	for _, images := range []string{`[]`, `[{"file_id":"file-foreign"}]`, `[{"image_url":"http://localhost/a"}]`, `[{"image_url":"https://example.com/a","file_id":"file-other"}]`} {
		if _, err := ParseImageOperationRequest(ImageEdit, "application/json", strings.NewReader(`{"model":"m","prompt":"x","images":`+images+`}`)); err == nil {
			t.Fatal("accepted invalid reference", images)
		}
	}
	raw, ct := editMultipart(t, map[string][]string{"model": {"m", "m2"}, "prompt": {"x"}}, nil)
	if _, err := ParseImageOperationRequest(ImageEdit, ct, bytes.NewReader(raw)); err == nil {
		t.Fatal("duplicate form fields accepted")
	}
	raw, ct = editMultipart(t, map[string][]string{"model": {"m"}, "prompt": {"x"}}, [][2]string{{"image", "not a PNG"}})
	if _, err := ParseImageOperationRequest(ImageEdit, ct, bytes.NewReader(raw)); err == nil {
		t.Fatal("fake image accepted")
	}
}
