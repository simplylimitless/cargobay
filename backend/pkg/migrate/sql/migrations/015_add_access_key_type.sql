-- Distinguishes short-lived login-session keys from user-managed personal
-- access tokens. Both were previously stored in access_keys with only a
-- free-text `name` ("login_session" vs. whatever the user typed) to tell
-- them apart, which meant every login session a user created showed up
-- alongside their real PATs in the API Tokens UI. Existing rows are
-- backfilled by name since that was the only signal available at the time.
ALTER TABLE access_keys ADD COLUMN IF NOT EXISTS key_type TEXT NOT NULL DEFAULT 'personal';
UPDATE access_keys SET key_type = 'session' WHERE name = 'login_session';
