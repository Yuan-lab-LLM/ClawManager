CREATE TABLE IF NOT EXISTS gateway_runtime_tasks (
  task_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
  instance_id INT NOT NULL,
  owner_user_id INT NOT NULL,
  generation INT NOT NULL,
  session_id VARCHAR(191) COLLATE utf8mb4_bin NOT NULL,
  task_id VARCHAR(191) COLLATE utf8mb4_bin NOT NULL,
  revoked BOOLEAN NOT NULL DEFAULT FALSE,
  finished BOOLEAN NOT NULL DEFAULT FALSE,
  runtime_acknowledged BOOLEAN NOT NULL DEFAULT FALSE,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_gateway_tasks_instance (instance_id, generation),
  CONSTRAINT fk_gateway_task_instance FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS gateway_runtime_task_models (
  task_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  model_id INT NOT NULL,
  PRIMARY KEY (task_key, model_id),
  KEY idx_gateway_task_model (model_id),
  CONSTRAINT fk_gateway_task_model_task FOREIGN KEY (task_key) REFERENCES gateway_runtime_tasks(task_key) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
