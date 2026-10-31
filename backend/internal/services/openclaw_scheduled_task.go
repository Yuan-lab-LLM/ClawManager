package services

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const OpenClawScheduledTaskFormat = "task/openclaw-cron@v1"

type openClawCronSchedule struct {
	Kind      string `json:"kind"`
	At        string `json:"at,omitempty"`
	EveryMs   *int64 `json:"everyMs,omitempty"`
	AnchorMs  *int64 `json:"anchorMs,omitempty"`
	Expr      string `json:"expr,omitempty"`
	Tz        string `json:"tz,omitempty"`
	StaggerMs *int64 `json:"staggerMs,omitempty"`
}

type openClawCronDelivery struct {
	Mode       string `json:"mode"`
	Channel    string `json:"channel,omitempty"`
	To         string `json:"to,omitempty"`
	BestEffort *bool  `json:"bestEffort,omitempty"`
}

type openClawCronPayload struct {
	Kind                       string `json:"kind"`
	Text                       string `json:"text,omitempty"`
	Message                    string `json:"message,omitempty"`
	Model                      string `json:"model,omitempty"`
	Thinking                   string `json:"thinking,omitempty"`
	TimeoutSeconds             *int   `json:"timeoutSeconds,omitempty"`
	AllowUnsafeExternalContent *bool  `json:"allowUnsafeExternalContent,omitempty"`
	Deliver                    *bool  `json:"deliver,omitempty"`
	Channel                    string `json:"channel,omitempty"`
	To                         string `json:"to,omitempty"`
	BestEffortDeliver          *bool  `json:"bestEffortDeliver,omitempty"`
}

type openClawCronConfig struct {
	Name           string                `json:"name"`
	Description    string                `json:"description,omitempty"`
	Enabled        *bool                 `json:"enabled,omitempty"`
	DeleteAfterRun *bool                 `json:"deleteAfterRun,omitempty"`
	AgentID        string                `json:"agentId,omitempty"`
	SessionKey     string                `json:"sessionKey,omitempty"`
	Schedule       openClawCronSchedule  `json:"schedule"`
	SessionTarget  string                `json:"sessionTarget"`
	WakeMode       string                `json:"wakeMode"`
	Payload        openClawCronPayload   `json:"payload"`
	Delivery       *openClawCronDelivery `json:"delivery,omitempty"`
}

// ValidateOpenClawCronConfig validates scheduled_task config against the OpenClaw
// cron schedule + payload + delivery acceptance benchmark.
func ValidateOpenClawCronConfig(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("scheduled task config is required")
	}

	var cfg openClawCronConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("scheduled task config must be valid JSON")
	}

	if strings.TrimSpace(cfg.Name) == "" {
		return fmt.Errorf("scheduled task name is required")
	}

	sessionTarget := strings.TrimSpace(cfg.SessionTarget)
	switch sessionTarget {
	case "main", "isolated":
	default:
		return fmt.Errorf("scheduled task sessionTarget must be main or isolated")
	}

	wakeMode := strings.TrimSpace(cfg.WakeMode)
	switch wakeMode {
	case "next-heartbeat", "now":
	default:
		return fmt.Errorf("scheduled task wakeMode must be next-heartbeat or now")
	}

	if err := validateOpenClawCronSchedule(cfg.Schedule); err != nil {
		return err
	}
	if err := validateOpenClawCronPayload(cfg.Payload, sessionTarget); err != nil {
		return err
	}
	if err := validateOpenClawCronDelivery(cfg.Delivery); err != nil {
		return err
	}
	return nil
}

func validateOpenClawCronSchedule(schedule openClawCronSchedule) error {
	kind := strings.TrimSpace(strings.ToLower(schedule.Kind))
	switch kind {
	case "at":
		if strings.TrimSpace(schedule.At) == "" {
			return fmt.Errorf("scheduled task schedule.at is required for kind=at")
		}
	case "every":
		if schedule.EveryMs == nil || *schedule.EveryMs <= 0 {
			return fmt.Errorf("scheduled task schedule.everyMs must be > 0 for kind=every")
		}
	case "cron":
		if strings.TrimSpace(schedule.Expr) == "" {
			return fmt.Errorf("scheduled task schedule.expr is required for kind=cron")
		}
	default:
		return fmt.Errorf("scheduled task schedule.kind must be at, every, or cron")
	}
	return nil
}

func validateOpenClawCronPayload(payload openClawCronPayload, sessionTarget string) error {
	kind := strings.TrimSpace(payload.Kind)
	switch kind {
	case "systemEvent":
		if strings.TrimSpace(payload.Text) == "" {
			return fmt.Errorf("scheduled task payload.text is required for kind=systemEvent")
		}
		if sessionTarget != "main" {
			return fmt.Errorf("scheduled task payload.kind=systemEvent requires sessionTarget=main")
		}
	case "agentTurn":
		if strings.TrimSpace(payload.Message) == "" {
			return fmt.Errorf("scheduled task payload.message is required for kind=agentTurn")
		}
		if sessionTarget != "isolated" {
			return fmt.Errorf("scheduled task payload.kind=agentTurn requires sessionTarget=isolated")
		}
	default:
		return fmt.Errorf("scheduled task payload.kind must be systemEvent or agentTurn")
	}

	if sessionTarget == "main" && kind != "systemEvent" {
		return fmt.Errorf("scheduled task sessionTarget=main requires payload.kind=systemEvent")
	}
	if sessionTarget == "isolated" && kind != "agentTurn" {
		return fmt.Errorf("scheduled task sessionTarget=isolated requires payload.kind=agentTurn")
	}
	return nil
}

func validateOpenClawCronDelivery(delivery *openClawCronDelivery) error {
	if delivery == nil {
		return nil
	}
	mode := strings.TrimSpace(strings.ToLower(delivery.Mode))
	switch mode {
	case "", "none", "announce":
		return nil
	case "webhook":
		to := strings.TrimSpace(delivery.To)
		if to == "" {
			return fmt.Errorf("scheduled task delivery.to is required for mode=webhook")
		}
		parsed, err := url.ParseRequestURI(to)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("scheduled task delivery.to must be an http(s) URL for mode=webhook")
		}
		return nil
	default:
		return fmt.Errorf("scheduled task delivery.mode must be none, announce, or webhook")
	}
}

func validateScheduledTaskEnvelope(envelope OpenClawConfigEnvelope) error {
	format := strings.TrimSpace(envelope.Format)
	if format != OpenClawScheduledTaskFormat {
		return fmt.Errorf("scheduled task format must be %s", OpenClawScheduledTaskFormat)
	}
	return ValidateOpenClawCronConfig(envelope.Config)
}
