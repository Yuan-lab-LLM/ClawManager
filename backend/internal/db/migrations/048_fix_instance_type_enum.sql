-- Keep the complete instance type set after the OpenCode and DeepSeek Harness
-- migrations. This must be a new migration so databases that already applied
-- 047_add_deepseek_harness_runtime.sql are repaired during upgrade as well.
ALTER TABLE instances
MODIFY COLUMN type ENUM(
  'openclaw',
  'ubuntu',
  'debian',
  'centos',
  'custom',
  'webtop',
  'hermes',
  'workbuddy',
  'opencode',
  'deepseek-harness'
) DEFAULT 'ubuntu';
