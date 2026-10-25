ALTER TABLE runtime_upgrade_items
  ADD COLUMN source_gateway_id VARCHAR(128) NULL AFTER runtime_pod_id,
  ADD COLUMN source_generation INT NULL AFTER source_gateway_id,
  ADD COLUMN source_pod_uid VARCHAR(128) NULL AFTER source_generation,
  ADD COLUMN source_deployment_name VARCHAR(255) NULL AFTER source_pod_uid,
  ADD COLUMN source_image_digest VARCHAR(255) NULL AFTER source_deployment_name;
