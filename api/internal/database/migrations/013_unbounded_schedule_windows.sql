ALTER TABLE schedule_snapshot DROP CONSTRAINT schedule_snapshot_check;
ALTER TABLE schedule_snapshot ADD CONSTRAINT schedule_snapshot_check CHECK (window_through >= window_from);

ALTER TABLE provider_snapshots DROP CONSTRAINT provider_snapshots_check;
ALTER TABLE provider_snapshots ADD CONSTRAINT provider_snapshots_check CHECK (window_through >= window_from);
