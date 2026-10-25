-- runtime_upgrade_items is an immutable rollout audit ledger.  Its instance_id
-- must keep the historical numeric identity after an instance is deleted, but
-- must not keep terminal rollout records from blocking an explicit deletion.
ALTER TABLE runtime_upgrade_items
  DROP FOREIGN KEY fk_runtime_upgrade_item_instance;
