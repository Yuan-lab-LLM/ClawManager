SET @dbname = DATABASE();

SET @indexname = 'idx_cost_records_instance_id';
SET @preparedStatement = (
  SELECT IF(
    EXISTS(
      SELECT 1 FROM information_schema.statistics
      WHERE table_schema = @dbname
        AND table_name = 'cost_records'
        AND index_name = @indexname
    ),
    'SELECT 1',
    'ALTER TABLE cost_records ADD INDEX idx_cost_records_instance_id (instance_id)'
  )
);
PREPARE stmt FROM @preparedStatement;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @indexname = 'idx_cost_records_session_id';
SET @preparedStatement = (
  SELECT IF(
    EXISTS(
      SELECT 1 FROM information_schema.statistics
      WHERE table_schema = @dbname
        AND table_name = 'cost_records'
        AND index_name = @indexname
    ),
    'SELECT 1',
    'ALTER TABLE cost_records ADD INDEX idx_cost_records_session_id (session_id)'
  )
);
PREPARE stmt FROM @preparedStatement;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @indexname = 'idx_model_invocations_instance_session';
SET @preparedStatement = (
  SELECT IF(
    EXISTS(
      SELECT 1 FROM information_schema.statistics
      WHERE table_schema = @dbname
        AND table_name = 'model_invocations'
        AND index_name = @indexname
    ),
    'SELECT 1',
    'ALTER TABLE model_invocations ADD INDEX idx_model_invocations_instance_session (instance_id, session_id, created_at)'
  )
);
PREPARE stmt FROM @preparedStatement;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
