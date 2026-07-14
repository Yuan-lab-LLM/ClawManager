package services

import "strings"

const (
	RuntimeGatewayBindingCreating = "creating"
	RuntimeGatewayBindingRunning  = "running"
	RuntimeGatewayBindingError    = "error"
	RuntimeGatewayBindingStopped  = "stopped"
)

type RuntimeGatewayLifecycleState struct {
	RawState      string
	BindingState  string
	InstanceState string
	Running       bool
	Recognized    bool
	EventType     string
	Message       *string
}

func NormalizeRuntimeGatewayLifecycle(rawState string, reportedMessage *string) RuntimeGatewayLifecycleState {
	raw := strings.ToLower(strings.TrimSpace(rawState))
	message := normalizedRuntimeGatewayMessage(reportedMessage)
	state := RuntimeGatewayLifecycleState{RawState: raw, Recognized: true}

	switch raw {
	case "running", "ready", "healthy":
		state.BindingState = RuntimeGatewayBindingRunning
		state.InstanceState = "running"
		state.Running = true
		state.EventType = "runtime.instance.running"
	case "starting", "creating", "pending":
		state.BindingState = RuntimeGatewayBindingCreating
		state.InstanceState = "creating"
		state.EventType = "runtime.instance.starting"
		state.Message = messageOrDefault(message, "runtime gateway starting")
	case "error", "failed", "failure", "errored":
		state.BindingState = RuntimeGatewayBindingError
		state.InstanceState = "error"
		state.EventType = "runtime.instance.error"
		state.Message = messageOrDefault(message, "runtime gateway reported "+raw)
	case "stopped", "deleted":
		state.BindingState = RuntimeGatewayBindingStopped
		state.InstanceState = "stopped"
		state.EventType = "runtime.instance.stopped"
		state.Message = message
	case "":
		state.Recognized = false
		state.BindingState = RuntimeGatewayBindingCreating
		state.InstanceState = "creating"
		state.EventType = "runtime.instance.starting"
		state.Message = messageOrDefault(message, "runtime gateway returned an empty state")
	default:
		state.Recognized = false
		state.BindingState = RuntimeGatewayBindingCreating
		state.InstanceState = "creating"
		state.EventType = "runtime.instance.starting"
		state.Message = messageOrDefault(message, "runtime gateway reported unrecognized state: "+raw)
	}
	return state
}

func (state RuntimeGatewayLifecycleState) CreateAccepted() bool {
	if !state.Recognized {
		return false
	}
	return state.BindingState == RuntimeGatewayBindingRunning || state.BindingState == RuntimeGatewayBindingCreating
}

func normalizedRuntimeGatewayMessage(message *string) *string {
	if message == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*message)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func messageOrDefault(message *string, fallback string) *string {
	if message != nil {
		return message
	}
	return &fallback
}
