package models

import "time"

// SystemImageSetting stores admin-managed runtime image overrides for instance types.
type SystemImageSetting struct {
	ID             int       `db:"id,primarykey,autoincrement" json:"id"`
	InstanceType   string    `db:"instance_type" json:"instance_type"`
	RuntimeType    string    `db:"runtime_type" json:"runtime_type"`
	RuntimeVariant string    `db:"runtime_variant" json:"runtime_variant,omitempty"`
	DisplayName    string    `db:"display_name" json:"display_name"`
	Image          string    `db:"image" json:"image"`
	IsEnabled      bool      `db:"is_enabled" json:"is_enabled"`
	CreatedAt      time.Time `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time `db:"updated_at" json:"updated_at"`
}

// TableName returns the table name for the SystemImageSetting model.
func (s SystemImageSetting) TableName() string {
	return "system_image_settings"
}
