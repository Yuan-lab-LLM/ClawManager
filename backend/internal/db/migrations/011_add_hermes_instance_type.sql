ALTER TABLE instances
MODIFY COLUMN type ENUM('openclaw', 'ubuntu', 'debian', 'centos', 'custom', 'webtop', 'hermes') DEFAULT 'ubuntu';
