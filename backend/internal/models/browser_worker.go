package models

import "time"

const (
	BrowserWorkerStatusDisabled     = "disabled"
	BrowserWorkerStatusProvisioning = "provisioning"
	BrowserWorkerStatusReady        = "ready"
	BrowserWorkerStatusDegraded     = "degraded"
	BrowserWorkerStatusError        = "error"
)

// InstanceBrowserWorker stores the desired and observed state of the optional
// per-instance visible browser component.
type InstanceBrowserWorker struct {
	InstanceID         int       `db:"instance_id" json:"instance_id"`
	Enabled            bool      `db:"enabled" json:"enabled"`
	Status             string    `db:"status" json:"status"`
	ResourceProfile    string    `db:"resource_profile" json:"resource_profile"`
	DisplayWidth       int       `db:"display_width" json:"display_width"`
	DisplayHeight      int       `db:"display_height" json:"display_height"`
	RetainProfile      bool      `db:"retain_profile" json:"retain_profile"`
	BrowserImage       string    `db:"browser_image" json:"browser_image,omitempty"`
	CDPProxyImage      string    `db:"cdp_proxy_image" json:"cdp_proxy_image,omitempty"`
	Generation         int       `db:"generation" json:"generation"`
	ObservedGeneration int       `db:"observed_generation" json:"observed_generation"`
	LastError          *string   `db:"last_error" json:"last_error,omitempty"`
	CreatedAt          time.Time `db:"created_at" json:"created_at"`
	UpdatedAt          time.Time `db:"updated_at" json:"updated_at"`
}

func (InstanceBrowserWorker) TableName() string { return "instance_browser_workers" }
