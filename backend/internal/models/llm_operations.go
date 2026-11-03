package models

import (
	"errors"
	"strings"
)

var ErrChatNotSupported = errors.New("当前模型不支持聊天，请选择聊天模型；图片生成模型需使用生图接口")
var ErrInvalidModelOperations = errors.New("invalid model operation configuration")

const (
	ModelKindChat  = "chat"
	ModelKindImage = "image"
	ModelKindOther = "other"
)

// Empty is the legacy catalog value. Unknown nonempty values fail closed.
func EffectiveLLMModelKind(model *LLMModel) string {
	if model == nil {
		return ModelKindOther
	}
	kind := strings.ToLower(strings.TrimSpace(model.ModelKind))
	if kind == "" {
		return ModelKindChat
	}
	return kind
}

func LLMChatAvailable(model *LLMModel) bool {
	return EffectiveLLMModelKind(model) == ModelKindChat
}

// ChatModels returns a fresh slice, preserving repository-owned collections.
func ChatModels(items []LLMModel) []LLMModel {
	result := make([]LLMModel, 0, len(items))
	for _, item := range items {
		if LLMChatAvailable(&item) {
			result = append(result, item)
		}
	}
	return result
}

func LLMNativeImagesAvailable(model *LLMModel) bool {
	if model == nil || EffectiveLLMModelKind(model) != ModelKindImage {
		return false
	}
	protocol, err := ResolveLLMProtocolType(model.ProviderType, model.ProtocolType)
	return err == nil && (protocol == ProtocolTypeOpenAI || protocol == ProtocolTypeOpenAICompatible)
}
