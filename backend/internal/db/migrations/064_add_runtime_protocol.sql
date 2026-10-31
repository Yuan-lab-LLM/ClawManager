-- No backfill: existing instances retain the legacy request contract.
-- Stored separately so ordinary instance updates cannot erase enrollment.
CREATE TABLE IF NOT EXISTS gateway_runtime_protocols (
  instance_id INT NOT NULL PRIMARY KEY,
  owner_user_id INT NOT NULL,
  generation INT NOT NULL,
  protocol INT NOT NULL,
  runtime_version VARCHAR(100) NOT NULL,
  adapter_version VARCHAR(100) NOT NULL,
  capabilities_json TEXT NOT NULL,
  reported_at DATETIME(6) NOT NULL,
  CONSTRAINT fk_gateway_runtime_protocol_instance FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
