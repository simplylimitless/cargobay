-- Username was already enforced unique at the DB level; email was not,
-- meaning two accounts could share an email even though the API now
-- rejects that at the application layer. Add the matching DB constraint
-- as defense-in-depth. This will fail if duplicate emails already exist --
-- resolve those manually before applying.
--
-- Guarded rather than a plain ALTER TABLE ... ADD CONSTRAINT: schema.sql
-- already declares email UNIQUE (implicitly naming the constraint
-- users_email_key), so on a database bootstrapped fresh -- where this
-- migration still runs, just after schema.sql -- the constraint already
-- exists and a bare ADD CONSTRAINT would fail with a duplicate-name error.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'users_email_key' AND conrelid = 'users'::regclass
    ) THEN
        ALTER TABLE users ADD CONSTRAINT users_email_key UNIQUE (email);
    END IF;
END $$;
