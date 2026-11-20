-- Backfill current spellings only. Older spellings that predate identity history
-- cannot be reconstructed safely and must never be guessed from a model suffix.
INSERT INTO llm_runtime_provider_refs (provider_id, model_ref, model_id)
SELECT CASE WHEN REGEXP_LIKE(provider_name, '^[A-Za-z0-9_.:-]+$', 'c')
            THEN LOWER(provider_name)
            ELSE CONCAT('cm-provider-v1-', SHA2(provider_name, 256)) END,
       CASE WHEN REGEXP_LIKE(model_name, '^[A-Za-z0-9_.:/-]+$', 'c')
                 AND LEFT(model_name, 1) <> '/' AND RIGHT(model_name, 1) <> '/'
                 AND INSTR(model_name, '//') = 0
            THEN LOWER(model_name)
            ELSE CONCAT('cm-model-v1-', SHA2(model_name, 256)) END,
       id
FROM (
  SELECT id, TRIM(display_name) AS model_name,
         REPLACE(REPLACE(TRIM(display_name), '/', '-'), CHAR(92), '-') AS provider_name
  FROM llm_models WHERE TRIM(display_name) <> ''
) AS current_names
WHERE NOT REGEXP_LIKE(provider_name, '^[A-Za-z0-9_.:-]+$', 'c')
   OR NOT REGEXP_LIKE(model_name, '^[A-Za-z0-9_.:/-]+$', 'c')
   OR LEFT(model_name, 1) = '/' OR RIGHT(model_name, 1) = '/'
   OR INSTR(model_name, '//') <> 0;
