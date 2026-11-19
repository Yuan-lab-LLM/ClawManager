-- Shared writer / exclusive rebuild locks contain no business data.
CREATE TABLE IF NOT EXISTS observation_summary_day_locks (
 day_slot SMALLINT UNSIGNED PRIMARY KEY
) ENGINE=InnoDB;
INSERT IGNORE INTO observation_summary_day_locks(day_slot)
 WITH RECURSIVE slots AS (SELECT 0 n UNION ALL SELECT n+1 FROM slots WHERE n<255) SELECT n FROM slots;
-- A trigger is required while old application versions can write audit_events.
-- The queue becomes visible atomically with its source transaction, so a lower
-- allocated ID committing after catchup cannot fall behind a high watermark.
CREATE TABLE IF NOT EXISTS observation_audit_pending (
 id BIGINT PRIMARY KEY
) ENGINE=InnoDB;
DROP TRIGGER IF EXISTS observation_audit_enqueue;
CREATE TRIGGER observation_audit_enqueue AFTER INSERT ON audit_events
 FOR EACH ROW INSERT IGNORE INTO observation_audit_pending(id) VALUES(NEW.id);
-- Trigger DDL drains preexisting source writers. Reopen the initial source
-- sweep to include rows committed between the earlier migration and this gate.
UPDATE observation_audit_backfill SET after_id=0,complete=FALSE WHERE source='audit_events';
CREATE TABLE IF NOT EXISTS observation_invocation_pending (id BIGINT PRIMARY KEY) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS observation_cost_pending (id BIGINT PRIMARY KEY) ENGINE=InnoDB;
DROP TRIGGER IF EXISTS observation_invocation_enqueue;
CREATE TRIGGER observation_invocation_enqueue AFTER INSERT ON model_invocations
 FOR EACH ROW INSERT IGNORE INTO observation_invocation_pending(id) VALUES(NEW.id);
DROP TRIGGER IF EXISTS observation_cost_enqueue;
CREATE TRIGGER observation_cost_enqueue AFTER INSERT ON cost_records
 FOR EACH ROW INSERT IGNORE INTO observation_cost_pending(id) VALUES(NEW.id);
UPDATE history_maintenance_cursors SET after_id=0 WHERE kind IN ('query_summary','query_cost_summary');
UPDATE observation_summary_state SET target_id=COALESCE((SELECT MAX(id) FROM model_invocations),0),ready=(target_id=0) WHERE id=1;
UPDATE observation_summary_state SET target_id=COALESCE((SELECT MAX(id) FROM cost_records),0),ready=(target_id=0) WHERE id=2;
