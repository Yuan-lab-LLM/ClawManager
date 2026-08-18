package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"clawreef/internal/config"
)

const skillScannerErrorBodyLimit = 4096

type SkillScannerClient interface {
	ScanArchive(ctx context.Context, fileName string, content []byte, options map[string]string) (string, map[string]interface{}, string, error)
	AvailableAnalyzers(ctx context.Context) ([]string, error)
}

type noopSkillScannerClient struct{}

func (n *noopSkillScannerClient) ScanArchive(ctx context.Context, fileName string, content []byte, options map[string]string) (string, map[string]interface{}, string, error) {
	return "", nil, "", fmt.Errorf("skill scanner is disabled")
}

func (n *noopSkillScannerClient) AvailableAnalyzers(ctx context.Context) ([]string, error) {
	return nil, fmt.Errorf("skill scanner is disabled")
}

type httpSkillScannerClient struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewSkillScannerClient(cfg config.SkillScannerConfig) SkillScannerClient {
	if !cfg.Enabled || strings.TrimSpace(cfg.BaseURL) == "" {
		return &noopSkillScannerClient{}
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &httpSkillScannerClient{
		baseURL: strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		apiKey:  strings.TrimSpace(cfg.APIKey),
		client:  &http.Client{Timeout: timeout},
	}
}

func (c *httpSkillScannerClient) ScanArchive(ctx context.Context, fileName string, content []byte, options map[string]string) (string, map[string]interface{}, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return "", nil, "", fmt.Errorf("failed to create skill scanner upload: %w", err)
	}
	if _, err := part.Write(content); err != nil {
		return "", nil, "", fmt.Errorf("failed to write skill scanner upload: %w", err)
	}
	_ = writer.WriteField("format", "json")
	if err := writer.Close(); err != nil {
		return "", nil, "", fmt.Errorf("failed to finalize skill scanner upload: %w", err)
	}

	endpoint, err := url.Parse(c.baseURL + "/scan-upload")
	if err != nil {
		return "", nil, "", fmt.Errorf("failed to build skill scanner url: %w", err)
	}
	query := endpoint.Query()
	applyScanUploadOptions(query, options)
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), &body)
	if err != nil {
		return "", nil, "", fmt.Errorf("failed to create skill scanner request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return "", nil, "", fmt.Errorf("skill scanner request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil, "", fmt.Errorf("%s", formatSkillScannerStatusError(resp.StatusCode, resp.Body))
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return "", nil, "", fmt.Errorf("failed to decode skill scanner response: %w", err)
	}
	riskLevel := normalizeScannerRiskLevel(raw)
	summary := extractScannerSummary(raw)
	return riskLevel, raw, summary, nil
}

func formatSkillScannerStatusError(statusCode int, body io.Reader) string {
	detail := ""
	if body != nil {
		raw, _ := io.ReadAll(io.LimitReader(body, skillScannerErrorBodyLimit))
		detail = strings.TrimSpace(string(raw))
		detail = strings.Join(strings.Fields(detail), " ")
		if len(detail) > 512 {
			detail = detail[:512]
		}
	}
	if detail == "" {
		return fmt.Sprintf("skill scanner returned status %d", statusCode)
	}
	return fmt.Sprintf("skill scanner returned status %d: %s", statusCode, detail)
}

func (c *httpSkillScannerClient) AvailableAnalyzers(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create skill scanner health request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("skill scanner health request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("skill scanner health returned status %d", resp.StatusCode)
	}

	var raw struct {
		Analyzers []string `json:"analyzers_available"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to decode skill scanner health response: %w", err)
	}
	result := make([]string, 0, len(raw.Analyzers))
	for _, item := range raw.Analyzers {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		item = strings.TrimSuffix(item, "_analyzer")
		result = append(result, item)
	}
	return result, nil
}

func applyScanUploadOptions(query url.Values, options map[string]string) {
	analyzers := splitAnalyzerOption(options["analyzers"])
	if hasAnalyzer(analyzers, "behavioral") {
		query.Set("use_behavioral", "true")
	}
	if hasAnalyzer(analyzers, "llm") || hasAnalyzer(analyzers, "meta") {
		query.Set("use_llm", "true")
		query.Set("llm_provider", "openai")
	}
}

func splitAnalyzerOption(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	items := strings.Split(value, ",")
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		result = append(result, item)
	}
	return result
}

func hasAnalyzer(items []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func normalizeScannerRiskLevel(raw map[string]interface{}) string {
	if safe, ok := readBool(raw["is_safe"]); ok && safe {
		return skillRiskNone
	}
	candidates := []string{
		readString(raw["risk_level"]),
		readString(raw["severity"]),
		readString(raw["verdict"]),
		readString(raw["max_severity"]),
	}
	if result, ok := raw["result"].(map[string]interface{}); ok {
		if safe, ok := readBool(result["is_safe"]); ok && safe {
			return skillRiskNone
		}
		candidates = append(candidates,
			readString(result["risk_level"]),
			readString(result["severity"]),
			readString(result["verdict"]),
			readString(result["max_severity"]),
		)
	}
	for _, value := range candidates {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "critical", "high":
			return skillRiskHigh
		case "medium", "moderate":
			return skillRiskMedium
		case "low", "warning":
			return skillRiskLow
		case "none", "clean", "safe", "pass", "info", "informational":
			return skillRiskNone
		}
	}
	return skillRiskUnknown
}

func extractScannerSummary(raw map[string]interface{}) string {
	candidates := []string{
		readString(raw["summary"]),
		readString(raw["message"]),
	}
	if result, ok := raw["result"].(map[string]interface{}); ok {
		candidates = append(candidates, readString(result["summary"]), readString(result["message"]))
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) != "" {
			return candidate
		}
	}
	return "Skill scanned by external skill-scanner service"
}

func readString(value interface{}) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func readBool(value interface{}) (bool, bool) {
	boolean, ok := value.(bool)
	return boolean, ok
}
