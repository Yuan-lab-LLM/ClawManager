package aigateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// LayaError contains only a safe machine code, never response text or credentials.
type LayaError struct{ Code string }

func (e *LayaError) Error() string { return e.Code }
func layaError(code string) error  { return &LayaError{Code: code} }

type LayaClassifier struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

var layaTiers = map[string]int{"simple": 1, "medium": 2, "complex": 3, "reasoning": 4}

func ValidateLayaURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return "", layaError("invalid_url")
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return "", layaError("invalid_url")
		}
	}
	if len(raw) > 500 || strings.EqualFold(u.Hostname(), "localhost") || strings.HasSuffix(strings.ToLower(u.Hostname()), ".localhost") {
		return "", layaError("invalid_url")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !layaAddressAllowed(ip) {
		return "", layaError("blocked_address")
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = ""
	return u.String(), nil
}

func layaAddressAllowed(ip net.IP) bool {
	// Private cluster/LAN addresses are intentional; metadata, loopback, multicast
	// and unspecified addresses are not. Rechecked after DNS, at connection time.
	return ip.IsGlobalUnicast() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.Equal(net.ParseIP("100.100.100.200")) && !ip.Equal(net.ParseIP("fd00:ec2::254"))
}

func layaDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, layaError("invalid_url")
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, layaError("connection_failed")
	}
	if len(ips) == 0 {
		return nil, layaError("connection_failed")
	}
	for _, ip := range ips {
		if !layaAddressAllowed(ip.IP) {
			return nil, layaError("blocked_address")
		}
	}
	for _, ip := range ips {
		conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, layaError("connection_failed")
}

func NewLayaClassifier(baseURL, key string, timeout time.Duration) (*LayaClassifier, error) {
	baseURL, err := ValidateLayaURL(baseURL)
	if err != nil {
		return nil, err
	}
	key = strings.TrimSpace(key)
	if len(key) == 0 || len(key) > 4096 {
		return nil, layaError("invalid_api_key")
	}
	for _, char := range key {
		if char < 33 || char > 126 {
			return nil, layaError("invalid_api_key")
		}
	}
	if timeout < 100*time.Millisecond || timeout > 30*time.Second {
		return nil, layaError("invalid_timeout")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = layaDialContext
	transport.TLSHandshakeTimeout = 3 * time.Second
	transport.MaxConnsPerHost = 4
	return &LayaClassifier{baseURL: baseURL, apiKey: key, client: &http.Client{
		Timeout: timeout, Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (l *LayaClassifier) Close() { l.client.CloseIdleConnections() }

func layaPayload(input RoutingClassificationInput) ([]byte, []string, error) {
	if len(input.Turns) == 0 || len(input.Turns) > 8 {
		return nil, nil, layaError("invalid_input")
	}
	turns := append([]RoutingTaskTurn(nil), input.Turns...)
	for _, turn := range turns {
		if turn.HasNonText || turn.Truncated {
			return nil, nil, layaError("unsupported_input")
		}
	}
	if strings.TrimSpace(turns[len(turns)-1].Text) == "" {
		return nil, nil, layaError("invalid_input")
	}
	warnings := []string{}
	var state []byte
	for {
		var err error
		state, err = json.Marshal(turns)
		if err != nil {
			return nil, nil, layaError("invalid_input")
		}
		// Conservative UTF-8 byte budget, NOT an exact tokenizer count. Reserve
		// space for questions/formatting under explicit 2048-token inference.
		// Never silently truncate the current task; oversized tasks fall back.
		if len(state) <= 1200 {
			break
		}
		if len(turns) == 1 {
			return nil, nil, layaError("input_too_long")
		}
		turns = turns[1:]
		warnings = []string{"laya_history_omitted"}
	}
	body, err := json.Marshal(map[string]any{
		"model": "multilingual", "state": string(state), "max_len": 2048, "head_max_len": layaHeadMaxLen,
		"questions": map[string]any{"tier": layaTierQuestion()},
	})
	return body, warnings, err
}

func (l *LayaClassifier) Classify(ctx context.Context, input RoutingClassificationInput) (RoutingClassification, error) {
	if err := ctx.Err(); err != nil {
		return RoutingClassification{}, err
	}
	body, warnings, err := layaPayload(input)
	if err != nil {
		return RoutingClassification{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.baseURL+"/v1/systemone", bytes.NewReader(body))
	if err != nil {
		return RoutingClassification{}, layaError("invalid_url")
	}
	req.Header.Set("Authorization", "Bearer "+l.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := l.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return RoutingClassification{}, ctx.Err()
		}
		var networkErr net.Error
		if errors.As(err, &networkErr) && networkErr.Timeout() {
			return RoutingClassification{}, layaError("timeout")
		}
		var safe *LayaError
		if errors.As(err, &safe) {
			return RoutingClassification{}, safe
		}
		return RoutingClassification{}, layaError("connection_failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return RoutingClassification{}, layaError(fmt.Sprintf("http_%d", resp.StatusCode))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil {
		if ctx.Err() != nil {
			return RoutingClassification{}, ctx.Err()
		}
		return RoutingClassification{}, layaError("response_read_failed")
	}
	if len(raw) > 65536 {
		return RoutingClassification{}, layaError("response_too_large")
	}
	var response struct {
		Answers map[string]struct {
			Type          string             `json:"type"`
			Choice        string             `json:"choice"`
			Confidence    *float64           `json:"confidence"`
			Probabilities map[string]float64 `json:"probabilities"`
		} `json:"answers"`
		Routing struct {
			Model string `json:"model"`
		} `json:"routing"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return RoutingClassification{}, layaError("invalid_response")
	}
	answer, ok := response.Answers["tier"]
	tier := layaTiers[answer.Choice]
	if !ok || tier == 0 || answer.Type != "choice" || response.Routing.Model != "multilingual" {
		return RoutingClassification{}, layaError("invalid_response")
	}
	if answer.Confidence != nil && (!finiteProbability(*answer.Confidence)) {
		return RoutingClassification{}, layaError("invalid_response")
	}
	if len(answer.Probabilities) != 4 {
		return RoutingClassification{}, layaError("invalid_response")
	}
	sum := 0.0
	for name, probability := range answer.Probabilities {
		if layaTiers[name] == 0 || !finiteProbability(probability) {
			return RoutingClassification{}, layaError("invalid_response")
		}
		// The declared choice must be a maximum-probability option. Small
		// tolerance allows the provider's four-decimal probability rounding.
		if probability > answer.Probabilities[answer.Choice]+0.0001 {
			return RoutingClassification{}, layaError("invalid_response")
		}
		sum += probability
	}
	if math.Abs(sum-1) > .02 {
		return RoutingClassification{}, layaError("invalid_response")
	}
	return RoutingClassification{RequiredTier: tier, Confidence: answer.Confidence, ConfidenceKind: "normalized_entropy",
		Probabilities: answer.Probabilities, Signals: []string{layaPromptVersion}, Warnings: warnings}, nil
}

func finiteProbability(n float64) bool {
	return !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0 && n <= 1
}
