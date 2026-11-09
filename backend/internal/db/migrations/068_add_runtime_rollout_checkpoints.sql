-- Additive execution state; do not overload error_message or OpenClaw preflight data.
CREATE TABLE IF NOT EXISTS runtime_rollout_checkpoints (
    rollout_id BIGINT NOT NULL PRIMARY KEY,
    state_json MEDIUMTEXT NOT NULL,
    updated_at DATETIME(6) NOT NULL
);
