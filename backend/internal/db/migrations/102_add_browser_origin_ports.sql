-- Assigned ports are permanent browser security origins. No foreign key or
-- cascading delete: deleting an instance must never make its origin reusable.
-- Free pool rows are seeded in bounded, ordered transactions by the registry.
-- identity_hash pins assignments to creation time and owner, not a reusable ID.
CREATE TABLE IF NOT EXISTS browser_origin_ports (
 port INT NOT NULL PRIMARY KEY,
 instance_id INT NULL,
 identity_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
 kind VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NULL,
 target VARCHAR(512) CHARACTER SET ascii COLLATE ascii_bin NULL,
 UNIQUE KEY uk_browser_origin_identity (instance_id, kind, target)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
