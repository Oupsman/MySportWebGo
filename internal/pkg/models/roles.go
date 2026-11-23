package models

import (
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The bundled Postgres creates one role, a superuser (POSTGRES_USER): connected with it, an SQL injection
// would read the files of the server or run commands (COPY ... PROGRAM). It can't lose its SUPERUSER
// attribute (the bootstrap user of the cluster), so the backend works with another role, without any
// privilege but owning the database and its objects; the superuser is left to the backups and the admin
// command line.

// IsSuperuser tells whether the role of the connection is a superuser
func (db *DB) IsSuperuser() (bool, error) {
	var super bool
	err := db.Raw("SELECT rolsuper FROM pg_roles WHERE rolname = current_user").Scan(&super).Error
	return super, err
}

// EnsureAppRole creates the role (or sets it again) the backend works with, logging in with password,
// without privileges, and gives it the current database, its public schema and all its objects. Run by a
// superuser at each start: the objects a restore created as the superuser go back to it.
func (db *DB) EnsureAppRole(role, password string) error {
	if role == "" || password == "" {
		return fmt.Errorf("the role of the application needs a name and a password")
	}
	// never logged: the statement holds the password
	quiet := db.Session(&gorm.Session{Logger: logger.Discard})
	var exists bool
	if err := quiet.Raw("SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = ?)", role).Scan(&exists).Error; err != nil {
		return err
	}
	verb := "ALTER"
	if !exists {
		verb = "CREATE"
	}
	// a utility statement takes no parameters: the server quotes the name and the password
	var statement string
	err := quiet.Raw("SELECT format('"+verb+" ROLE %I WITH LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE "+
		"NOREPLICATION NOBYPASSRLS', ?::text, ?::text)", role, password).Scan(&statement).Error
	if err != nil {
		return err
	}
	if err := quiet.Exec(statement).Error; err != nil {
		return fmt.Errorf("%s ROLE %s failed: %w", verb, role, err)
	}
	return db.Transaction(func(tx *gorm.DB) error {
		// the name of the role reaches the block as a setting: a DO block takes no parameters either
		if err := tx.Exec("SELECT set_config('mysportweb.app_role', ?, true)", role).Error; err != nil {
			return err
		}
		return tx.Exec(giveToAppRole).Error
	})
}

// giveToAppRole gives the role of mysportweb.app_role the current database, the public schema and the
// objects in it it doesn't own yet: tables, views, sequences not tied to a column (those follow their table),
// functions, types
const giveToAppRole = `DO $$
DECLARE
	app text := current_setting('mysportweb.app_role');
	r record;
BEGIN
	EXECUTE format('ALTER DATABASE %I OWNER TO %I', current_database(), app);
	EXECUTE format('ALTER SCHEMA public OWNER TO %I', app);
	FOR r IN
		SELECT c.relname, c.relkind FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p', 'v', 'm', 'f', 'S')
			AND pg_get_userbyid(c.relowner) <> app
			AND NOT (c.relkind = 'S' AND EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = c.oid
				AND d.classid = 'pg_class'::regclass AND d.deptype IN ('a', 'i')))
	LOOP
		EXECUTE format(CASE r.relkind WHEN 'S' THEN 'ALTER SEQUENCE public.%I OWNER TO %I'
			WHEN 'v' THEN 'ALTER VIEW public.%I OWNER TO %I'
			WHEN 'm' THEN 'ALTER MATERIALIZED VIEW public.%I OWNER TO %I'
			WHEN 'f' THEN 'ALTER FOREIGN TABLE public.%I OWNER TO %I'
			ELSE 'ALTER TABLE public.%I OWNER TO %I' END, r.relname, app);
	END LOOP;
	FOR r IN
		SELECT p.oid::regprocedure AS routine FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname = 'public' AND pg_get_userbyid(p.proowner) <> app
	LOOP
		EXECUTE format('ALTER ROUTINE %s OWNER TO %I', r.routine, app);
	END LOOP;
	FOR r IN
		SELECT t.typname FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace
		WHERE n.nspname = 'public' AND t.typtype IN ('e', 'd', 'r', 'm')
			AND pg_get_userbyid(t.typowner) <> app
	LOOP
		EXECUTE format('ALTER TYPE public.%I OWNER TO %I', r.typname, app);
	END LOOP;
END $$`
