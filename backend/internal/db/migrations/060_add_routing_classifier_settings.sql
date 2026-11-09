-- Manual classifier configuration/testing only; does not enable production routing.
CREATE TABLE IF NOT EXISTS routing_classifier_settings (
  id INT PRIMARY KEY,
  base_url VARCHAR(500) NOT NULL DEFAULT '',
  timeout_ms INT NOT NULL DEFAULT 5000,
  encrypted_api_key TEXT NOT NULL,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
