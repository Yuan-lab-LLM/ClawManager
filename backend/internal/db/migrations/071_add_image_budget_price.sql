-- NULL deliberately blocks image execution; do not infer prices from chat tokens.
SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()
   AND TABLE_NAME = 'llm_models' AND COLUMN_NAME = 'image_price_per_image') = 0,
  'ALTER TABLE llm_models ADD COLUMN image_price_per_image DECIMAL(18,8) NULL',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
