-- Username was already enforced unique at the DB level; email was not,
-- meaning two accounts could share an email even though the API now
-- rejects that at the application layer. Add the matching DB constraint
-- as defense-in-depth. This will fail if duplicate emails already exist --
-- resolve those manually before applying.
ALTER TABLE users ADD CONSTRAINT users_email_key UNIQUE (email);
