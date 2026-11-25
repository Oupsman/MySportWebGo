-- +goose Up
-- the imports of an account from the zip of an export (models.AccountImport): one not finished at a time per
-- user, resumed at the next start when the server stops meanwhile. zip_path: the zip, removed once finished.
CREATE TABLE account_imports (
    id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    finished_at timestamp with time zone,
    user_id bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    status text NOT NULL,
    zip_path text NOT NULL DEFAULT '',
    with_profile boolean NOT NULL DEFAULT false,
    format integer NOT NULL DEFAULT 0,
    total integer NOT NULL DEFAULT 0,
    done integer NOT NULL DEFAULT 0,
    report jsonb,
    error text NOT NULL DEFAULT ''
);
CREATE INDEX account_imports_user_id ON account_imports (user_id, created_at);
-- the imports to resume at the start
CREATE INDEX account_imports_unfinished ON account_imports (status) WHERE status IN ('pending', 'running');

-- +goose Down
DROP TABLE account_imports;
