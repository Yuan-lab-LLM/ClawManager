package models

import "errors"

var ErrVisionNotSupported = errors.New("当前模型未启用图片理解，请选择支持图片的模型或使用 Auto")
var ErrNoVisionModel = errors.New("当前没有可用且已授权的图片理解模型，请管理员确认图片理解开关及模型协议")
var ErrInvalidImageInput = errors.New("invalid image input")

// LLMVisionSupported is shared by routing and the runtime catalog. Unknown
// capability is not permission to send images; explicit admin choices win.
func LLMVisionSupported(model *LLMModel) bool {
	if !LLMChatAvailable(model) {
		return false
	}
	if model.VisionSupportOverride != nil {
		return *model.VisionSupportOverride
	}
	return model.SupportsVision && model.CapabilitySource == "provider_metadata"
}

// LLMVisionInputAvailable is used for runtime advertisements. A manual
// capability assertion cannot make an unimplemented transport available.
func LLMVisionInputAvailable(model *LLMModel) bool {
	if !LLMVisionSupported(model) {
		return false
	}
	switch ResolveLLMProtocolTypeOrDefault(model.ProviderType, model.ProtocolType) {
	case ProtocolTypeOpenAI, ProtocolTypeOpenAICompatible, ProtocolTypeAnthropic:
		return true
	default:
		return false
	}
}
