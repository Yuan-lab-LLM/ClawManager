-- Existing single-column trace indexes remain useful. Add ordering/fee keys only
-- when their exact ordered columns are not already present under any name.
SET @query_index_exists=(SELECT COUNT(*) FROM (SELECT INDEX_NAME,GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX) cols FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='cost_records' GROUP BY INDEX_NAME) indexes_found WHERE cols='instance_id,session_id,invocation_id,trace_id');
SET @query_index_sql=IF(@query_index_exists=0,'ALTER TABLE cost_records ADD INDEX idx_cost_session_invocation_trace(instance_id,session_id,invocation_id,trace_id)','SELECT 1');
PREPARE query_index_stmt FROM @query_index_sql;
EXECUTE query_index_stmt;
DEALLOCATE PREPARE query_index_stmt;
SET @query_index_exists=(SELECT COUNT(*) FROM (SELECT INDEX_NAME,GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX) cols FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='observation_invocation_summaries' GROUP BY INDEX_NAME) indexes_found WHERE cols='instance_id,created_at,session_id,id');
SET @query_index_sql=IF(@query_index_exists=0,'ALTER TABLE observation_invocation_summaries ADD INDEX idx_summary_latest(instance_id,created_at,session_id,id)','SELECT 1');
PREPARE query_index_stmt FROM @query_index_sql;
EXECUTE query_index_stmt;
DEALLOCATE PREPARE query_index_stmt;
SET @query_index_exists=(SELECT COUNT(*) FROM (SELECT INDEX_NAME,GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX) cols FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='history_archive_records' GROUP BY INDEX_NAME) indexes_found WHERE cols='kind,trace_id,created_at,original_id');
SET @query_index_sql=IF(@query_index_exists=0,'ALTER TABLE history_archive_records ADD INDEX idx_history_kind_trace_time(kind,trace_id,created_at,original_id)','SELECT 1');
PREPARE query_index_stmt FROM @query_index_sql;
EXECUTE query_index_stmt;
DEALLOCATE PREPARE query_index_stmt;
SET @query_index_exists=(SELECT COUNT(*) FROM (SELECT INDEX_NAME,GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX) cols FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='cost_records' GROUP BY INDEX_NAME) indexes_found WHERE cols='invocation_id,instance_id,session_id,trace_id');
SET @query_index_sql=IF(@query_index_exists=0,'ALTER TABLE cost_records ADD INDEX idx_cost_invocation_scope(invocation_id,instance_id,session_id,trace_id)','SELECT 1');
PREPARE query_index_stmt FROM @query_index_sql;
EXECUTE query_index_stmt;
DEALLOCATE PREPARE query_index_stmt;
