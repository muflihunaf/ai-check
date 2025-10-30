BEGIN;

ALTER TABLE verification_logs
    ADD COLUMN IF NOT EXISTS sha1_hash VARCHAR(40);

UPDATE verification_logs
SET sha1_hash = lower(NULLIF(regexp_replace(details, '.*hash:([0-9a-fA-F]{40}).*', '\1'), details))
WHERE sha1_hash IS NULL
  AND details ~ 'hash:[0-9a-fA-F]{40}';

ALTER TABLE verification_logs
    ALTER COLUMN sha1_hash SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS verification_logs_user_hash_uq
    ON verification_logs (user_id, sha1_hash);

COMMIT;
