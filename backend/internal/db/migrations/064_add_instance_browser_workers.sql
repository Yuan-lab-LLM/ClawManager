CREATE TABLE IF NOT EXISTS instance_browser_workers (
    instance_id INT NOT NULL PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    status VARCHAR(32) NOT NULL DEFAULT 'disabled',
    resource_profile VARCHAR(32) NOT NULL DEFAULT 'standard',
    display_width INT NOT NULL DEFAULT 1440,
    display_height INT NOT NULL DEFAULT 900,
    retain_profile BOOLEAN NOT NULL DEFAULT TRUE,
    browser_image VARCHAR(512) NOT NULL DEFAULT 'clawmanager/browser-worker:0.1.0',
    cdp_proxy_image VARCHAR(512) NOT NULL DEFAULT 'clawmanager/cdp-proxy:0.1.0',
    generation INT NOT NULL DEFAULT 1,
    observed_generation INT NOT NULL DEFAULT 0,
    last_error TEXT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_instance_browser_workers_instance
        FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
    INDEX idx_instance_browser_workers_reconcile (enabled, status, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
