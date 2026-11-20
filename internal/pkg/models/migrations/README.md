# Schema migrations

Every change of the database schema is a file here, applied once by the backend at its start, in order of
version: `NNNNN_what.sql`, `NNNNN` the next number after the last file (5 digits, the version 1 is the
baseline in `../schema/baseline.sql`, applied from `../migrate.go`).

```sql
-- +goose Up
ALTER TABLE activities ADD COLUMN cadence_zones jsonb;

-- +goose Down
ALTER TABLE activities DROP COLUMN cadence_zones;
```

* Change the model and add its migration in the same commit: `TestSchemaMatchesModels` fails on a field of a
  model without its column.
* Never edit a migration once released: the databases that applied it won't apply it again.
* Each migration runs in a transaction. A statement that can't (`CREATE INDEX CONCURRENTLY`) needs
  `-- +goose NO TRANSACTION` at the top of the file.
* The backend refuses to start on a database migrated by a newer version: going back to an older version
  needs the backup of the database made before the upgrade (or the Down of the migrations, by hand).
