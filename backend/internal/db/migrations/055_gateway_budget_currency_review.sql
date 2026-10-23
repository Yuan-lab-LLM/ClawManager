-- Restart-safe after DDL autocommit; only explicit administrator review/reset
-- sets reviewed_at. Replaying the backfill never overwrites that decision.
SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()
   AND TABLE_NAME = 'gateway_keys' AND COLUMN_NAME = 'budget_currency_review_required') = 0,
  'ALTER TABLE gateway_keys ADD COLUMN budget_currency_review_required BOOLEAN NOT NULL DEFAULT FALSE',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()
   AND TABLE_NAME = 'gateway_keys' AND COLUMN_NAME = 'budget_currency_reviewed_at') = 0,
  'ALTER TABLE gateway_keys ADD COLUMN budget_currency_reviewed_at TIMESTAMP NULL',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

UPDATE gateway_keys k SET k.budget_currency_review_required = TRUE
WHERE k.budget_currency_reviewed_at IS NULL
  AND EXISTS (
    SELECT 1 FROM cost_records c WHERE c.gateway_key_id = k.id
      AND UPPER(COALESCE(NULLIF(TRIM(c.currency), ''), 'USD')) <> 'USD'
  );
