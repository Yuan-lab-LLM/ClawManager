package models

type RoutingClassifierSetting struct {
	ID              int    `db:"id" json:"-"`
	BaseURL         string `db:"base_url" json:"base_url"`
	TimeoutMS       int    `db:"timeout_ms" json:"timeout_ms"`
	EncryptedAPIKey string `db:"encrypted_api_key" json:"-"`
	RoutingMode     string `db:"routing_mode" json:"routing_mode"`
}
