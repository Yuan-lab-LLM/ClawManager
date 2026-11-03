-- Existing jobs were written by the legacy synchronous record writer. Do not
-- replay those rows: only new jobs with a captured snapshot enter the outbox.
SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()
   AND TABLE_NAME = 'gateway_image_jobs' AND COLUMN_NAME = 'analytics_context') = 0,
  'ALTER TABLE gateway_image_jobs ADD COLUMN analytics_context LONGTEXT NULL', 'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()
   AND TABLE_NAME = 'gateway_image_jobs' AND COLUMN_NAME = 'analytics_outcome') = 0,
  'ALTER TABLE gateway_image_jobs ADD COLUMN analytics_outcome LONGTEXT NULL', 'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()
   AND TABLE_NAME = 'gateway_image_jobs' AND COLUMN_NAME = 'analytics_recorded') = 0,
  'ALTER TABLE gateway_image_jobs ADD COLUMN analytics_recorded BOOLEAN NOT NULL DEFAULT TRUE', 'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE()
   AND TABLE_NAME = 'gateway_image_jobs' AND INDEX_NAME = 'idx_image_job_analytics') = 0,
  'ALTER TABLE gateway_image_jobs ADD KEY idx_image_job_analytics (analytics_recorded,state,updated_at)', 'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
