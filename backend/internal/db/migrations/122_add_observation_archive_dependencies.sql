CREATE TABLE IF NOT EXISTS observability_archive_dependencies (
 parent_key VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 child_key VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 PRIMARY KEY(parent_key,child_key),
 KEY idx_observation_archive_child(child_key,parent_key)
) ENGINE=InnoDB;
