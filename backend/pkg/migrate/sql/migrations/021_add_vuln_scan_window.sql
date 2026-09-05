-- Optional off-hours window restricting when the automatic vulnerability
-- rescan scheduler (see vulnerability.Rescanner.StartScheduler) is allowed to
-- run. Both NULL (the default) means no restriction -- the scheduler may run
-- at any hour, matching pre-existing behavior. Hours are 0-23, server-local,
-- and off_hours_end_hour may be less than off_hours_start_hour to express a
-- window that wraps past midnight (e.g. 22 -> 6).
ALTER TABLE vulnerability_scan_settings
    ADD COLUMN IF NOT EXISTS off_hours_start_hour SMALLINT,
    ADD COLUMN IF NOT EXISTS off_hours_end_hour   SMALLINT;
