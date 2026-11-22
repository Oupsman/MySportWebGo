-- +goose Up
-- a deleted user's invitations stay in the history of the admins, without them
ALTER TABLE invitations ALTER COLUMN created_by_id DROP NOT NULL;
ALTER TABLE invitations DROP CONSTRAINT fk_invitations_created_by;
ALTER TABLE invitations ADD CONSTRAINT fk_invitations_created_by FOREIGN KEY (created_by_id)
    REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE invitations DROP CONSTRAINT fk_invitations_used_by;
ALTER TABLE invitations ADD CONSTRAINT fk_invitations_used_by FOREIGN KEY (used_by_id)
    REFERENCES users(id) ON DELETE SET NULL;

-- a disabled user can't log in, their sessions and device tokens are refused; their data stays
ALTER TABLE users ADD COLUMN disabled_at timestamp with time zone;

-- the two-factor authentication (TOTP): the secret, encrypted with SECRET_KEY, set at its setup and enabled
-- once a code was checked; the last period whose code was used (a code is used once); the hashes of the
-- recovery codes, comma separated
ALTER TABLE users ADD COLUMN totp_secret text NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN totp_enabled_at timestamp with time zone;
ALTER TABLE users ADD COLUMN totp_last_step bigint NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN totp_recovery text NOT NULL DEFAULT '';

-- the links an admin creates for a user to choose a new password: single use, the code shown once (its hash
-- kept), valid for a day
CREATE TABLE password_resets (
    id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    user_id bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_by_id bigint REFERENCES users (id) ON DELETE SET NULL,
    hash text NOT NULL UNIQUE,
    expires_at timestamp with time zone NOT NULL,
    used_at timestamp with time zone
);
CREATE INDEX password_resets_user_id ON password_resets (user_id);

-- +goose Down
DROP TABLE password_resets;
ALTER TABLE users DROP COLUMN totp_recovery;
ALTER TABLE users DROP COLUMN totp_last_step;
ALTER TABLE users DROP COLUMN totp_enabled_at;
ALTER TABLE users DROP COLUMN totp_secret;
ALTER TABLE users DROP COLUMN disabled_at;
ALTER TABLE invitations DROP CONSTRAINT fk_invitations_used_by;
ALTER TABLE invitations ADD CONSTRAINT fk_invitations_used_by FOREIGN KEY (used_by_id) REFERENCES users(id);
ALTER TABLE invitations DROP CONSTRAINT fk_invitations_created_by;
ALTER TABLE invitations ADD CONSTRAINT fk_invitations_created_by FOREIGN KEY (created_by_id) REFERENCES users(id);
DELETE FROM invitations WHERE created_by_id IS NULL;
ALTER TABLE invitations ALTER COLUMN created_by_id SET NOT NULL;
