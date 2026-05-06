-- Migration 015: Add node_name column to instances table
-- This allows pods to be pinned to a specific worker node via NodeSelector,
-- ensuring hostPath PVC data is always accessible after pod restarts.
ALTER TABLE instances ADD COLUMN node_name VARCHAR(255) DEFAULT NULL AFTER pod_ip;

