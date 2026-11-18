-- Additive real observations only. No lifecycle or conversation data is stored.
CREATE TABLE IF NOT EXISTS workspace_dashboard_observations (
 user_id INT NOT NULL,
 bucket_start DATETIME NOT NULL,
 observed_at DATETIME(6) NOT NULL,
 snapshot_json JSON NOT NULL,
 PRIMARY KEY (user_id,bucket_start),
 KEY idx_workspace_observation_retention (bucket_start)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
