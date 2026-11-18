-- Retain exact legacy provider/model identities, including deleted deployments.
CREATE TABLE llm_runtime_provider_refs (
  provider_id VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  model_ref VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  model_id INT NOT NULL,
  PRIMARY KEY (provider_id, model_ref),
  INDEX idx_runtime_provider_model (model_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
INSERT INTO llm_runtime_provider_refs (provider_id, model_ref, model_id)
SELECT LOWER(REPLACE(REPLACE(TRIM(display_name), '/', '-'), CHAR(92), '-')),
       LOWER(TRIM(display_name)), id
FROM llm_models WHERE TRIM(display_name) <> '';
