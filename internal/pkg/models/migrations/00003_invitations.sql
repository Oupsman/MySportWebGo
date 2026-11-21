-- +goose Up
-- the invitations of the registrations by invitation (REGISTRATION=invite), created by the admins
CREATE TABLE invitations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamp with time zone,
    created_by_id bigint NOT NULL,
    hash text NOT NULL,
    hint text,
    email text,
    expires_at timestamp with time zone NOT NULL,
    used_at timestamp with time zone,
    used_by_id bigint,
    CONSTRAINT invitations_pkey PRIMARY KEY (id),
    CONSTRAINT fk_invitations_created_by FOREIGN KEY (created_by_id) REFERENCES users(id),
    CONSTRAINT fk_invitations_used_by FOREIGN KEY (used_by_id) REFERENCES users(id)
);
CREATE UNIQUE INDEX idx_invitations_hash ON invitations USING btree (hash);
CREATE INDEX idx_invitations_created_by_id ON invitations USING btree (created_by_id);

-- +goose Down
DROP TABLE invitations;
