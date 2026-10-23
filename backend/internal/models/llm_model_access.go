package models

import "errors"

const ModelVisibilityAll = "all"
const ModelVisibilitySelected = "selected_users"

var ErrModelUnavailable = errors.New("当前模型不可用，请重新选择模型")
var ErrModelAccessConflict = errors.New("模型授权已被其他管理员修改，请刷新后重试")
var ErrInvalidModelAccess = errors.New("模型可见范围无效，请选择有效用户")

// Email is a display/search attribute, never the authorization identity.
func (m *LLMModel) VisibleTo(userID int) bool {
	if m == nil || userID <= 0 {
		return false
	}
	switch m.VisibilityScope {
	case "", ModelVisibilityAll: // Empty supports legacy in-memory callers only.
		return true
	case ModelVisibilitySelected:
		for _, id := range m.AllowedUserIDs {
			if id == userID {
				return true
			}
		}
	}
	return false
}
