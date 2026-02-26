ALTER TABLE IF EXISTS workstations
  ADD COLUMN IF NOT EXISTS rustdesk TEXT;

ALTER TABLE IF EXISTS candidate_workstation_stagings
  ADD COLUMN IF NOT EXISTS rustdesk_id TEXT;

ALTER TABLE IF EXISTS network_candidate_ws_stagings
  ADD COLUMN IF NOT EXISTS rustdesk_id TEXT;
