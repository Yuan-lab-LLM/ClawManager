-- Old chat/risk writers are visible atomically in a bounded catchup queue.
CREATE TABLE IF NOT EXISTS observation_chat_pending (id BIGINT PRIMARY KEY) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS observation_risk_pending (id BIGINT PRIMARY KEY) ENGINE=InnoDB;
DROP TRIGGER IF EXISTS observation_chat_enqueue;
CREATE TRIGGER observation_chat_enqueue AFTER INSERT ON chat_messages
 FOR EACH ROW INSERT IGNORE INTO observation_chat_pending(id) VALUES(NEW.id);
DROP TRIGGER IF EXISTS observation_risk_enqueue;
CREATE TRIGGER observation_risk_enqueue AFTER INSERT ON risk_hits
 FOR EACH ROW INSERT IGNORE INTO observation_risk_pending(id) VALUES(NEW.id);
