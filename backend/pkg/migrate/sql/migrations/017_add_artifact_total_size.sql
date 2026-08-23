-- Add total_size column to artifacts table to track the combined size of
-- manifest and all layers/blobs. For non-container artifacts (npm, PyPI,
-- etc.) this is the same as size. For Docker/OCI images, total_size sums
-- the manifest size plus all layer sizes.
-- See: https://github.com/simplylimitless/cargobay/issues/XXX

ALTER TABLE artifacts
    ADD COLUMN IF NOT EXISTS total_size BIGINT NOT NULL DEFAULT 0;
