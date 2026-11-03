-- Empty by default. No instance/image/token is modified or upgraded.
CREATE TABLE IF NOT EXISTS gateway_runtime_upgrades (
 instance_id INT NOT NULL PRIMARY KEY,
 owner_user_id INT NOT NULL,
 generation INT NOT NULL,
 operation_id CHAR(32) NOT NULL,
 state VARCHAR(32) NOT NULL,
 created_at DATETIME(6) NOT NULL,
 updated_at DATETIME(6) NOT NULL,
 CONSTRAINT fk_runtime_upgrade_instance FOREIGN KEY(instance_id) REFERENCES instances(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
