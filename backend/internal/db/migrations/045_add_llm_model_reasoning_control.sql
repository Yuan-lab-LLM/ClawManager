ALTER TABLE llm_models
  ADD COLUMN reasoning_enabled BOOLEAN NOT NULL DEFAULT FALSE AFTER provider_model_name;
