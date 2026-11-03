-- Empty preserves the deployment default until an administrator chooses a mode.
ALTER TABLE routing_classifier_settings
  ADD COLUMN routing_mode VARCHAR(16) NOT NULL DEFAULT '';
