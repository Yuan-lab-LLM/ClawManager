UPDATE instance_skills isk
JOIN skills s ON s.id = isk.skill_id
SET isk.source_type = 'injected_by_clawmanager'
WHERE isk.source_type = 'discovered_in_instance'
  AND s.source_type = 'uploaded'
  AND s.visibility = 'public'
  AND isk.status = 'active';
