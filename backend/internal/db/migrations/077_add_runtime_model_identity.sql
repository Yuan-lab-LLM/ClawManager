ALTER TABLE llm_models ADD COLUMN runtime_model_id VARCHAR(64) NOT NULL DEFAULT '';
UPDATE llm_models SET runtime_model_id = CONCAT('cm-deploy-v1-', REPLACE(UUID(), '-', '')) WHERE runtime_model_id = '';
ALTER TABLE llm_models ADD UNIQUE KEY uk_llm_runtime_model_id (runtime_model_id);

-- No foreign key: deleted deployments retain their alias reservations. An old
-- session must never silently bind to a newly created deployment with that name.
CREATE TABLE llm_runtime_model_aliases (
  alias VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL PRIMARY KEY,
  model_id INT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_runtime_alias_model (model_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
INSERT INTO llm_runtime_model_aliases (alias, model_id)
SELECT LOWER(TRIM(display_name)), id FROM llm_models;
INSERT INTO llm_runtime_model_aliases (alias, model_id)
SELECT runtime_model_id, id FROM llm_models;
INSERT INTO llm_runtime_model_aliases (alias, model_id)
SELECT CONCAT('cm-model-v1-', SHA2(TRIM(display_name), 256)), id FROM llm_models;
