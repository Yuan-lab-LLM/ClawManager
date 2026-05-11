-- Allow fractional CPU cores for instances and quotas
-- Created: 2026-04-14

ALTER TABLE instances MODIFY COLUMN cpu_cores DECIMAL(10,2) NOT NULL;
ALTER TABLE user_quotas MODIFY COLUMN max_cpu_cores DECIMAL(10,2) DEFAULT 40;
