USE gke_version_monitor;
ALTER TABLE clusters ADD COLUMN sa_key_json TEXT NULL AFTER name;
