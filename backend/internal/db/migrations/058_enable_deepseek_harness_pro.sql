-- Add DeepSeek Harness Pro to the stock northbound policy without overriding
-- an administrator's deliberately customized Pro allow-list.
UPDATE northbound_admin_settings
SET allowed_pro_types = JSON_ARRAY_APPEND(allowed_pro_types, '$', 'deepseek-harness'),
    updated_at = UTC_TIMESTAMP(6)
WHERE id = 1
  AND NOT JSON_CONTAINS(allowed_pro_types, JSON_QUOTE('deepseek-harness'))
  AND JSON_LENGTH(allowed_pro_types) = 4
  AND JSON_CONTAINS(allowed_pro_types, JSON_QUOTE('openclaw'))
  AND JSON_CONTAINS(allowed_pro_types, JSON_QUOTE('hermes'))
  AND JSON_CONTAINS(allowed_pro_types, JSON_QUOTE('opencode'))
  AND JSON_CONTAINS(allowed_pro_types, JSON_QUOTE('workbuddy'));
