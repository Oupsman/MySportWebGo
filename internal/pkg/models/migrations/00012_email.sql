-- +goose Up
-- when the user confirmed their email address, NULL until they do: nothing is sent to an unconfirmed one
-- but its confirmation
ALTER TABLE users ADD COLUMN email_verified_at timestamp with time zone;

-- the confirmations of the email addresses sent, single use: the address confirmed (the current one, or a
-- new one the user changes to once confirmed), its code hashed
CREATE TABLE email_verifications (
    id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    user_id bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    email text NOT NULL,
    hash text NOT NULL UNIQUE,
    expires_at timestamp with time zone NOT NULL,
    used_at timestamp with time zone
);
CREATE INDEX email_verifications_user_id ON email_verifications (user_id);

-- +goose Down
DROP TABLE email_verifications;
ALTER TABLE users DROP COLUMN email_verified_at;
