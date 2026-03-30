-- Repair previously shipped broken admin seed hashes without touching
-- user-changed passwords.
UPDATE users
SET password_hash = '$2a$10$pbenze514mwv3pvQySQBVOsF5J4DBXL2kVo1hLa8JFhQu5x3AKvBi'
WHERE username = 'admin'
  AND password_hash = '$2a$10$N9qo8uLOickgx2ZMRZoMy.MqrzL9wGC3qD3Q.ZHqQH6t3q7l1L5uG';
