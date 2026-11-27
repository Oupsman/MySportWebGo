# Installing MySportWeb

This guide runs MySportWeb with Docker Compose: the frontend (nginx, which forwards `/api` and `/MEDIA` to the
backend), the backend, a nightly backup service, and an optional Postgres. What the app does: see the
[README](README.md).

* [Quick start](#quick-start)
* [Configuration](#configuration)
* [Accounts and administration](#accounts-and-administration)
* [Backups and restore](#backups-and-restore)
* [Emails](#emails)
* [Notifications](#notifications)
* [Monitoring](#monitoring)
* [Upgrades](#upgrades)
* [Auditing an instance](#auditing-an-instance)

## Quick start

```sh
cp compose.env.sample .env   # then set MSW_TAG, MSW_DOMAIN, MSW_SECRET_KEY and MSW_DBPASSWORD
docker compose up -d         # the app is on http://localhost:8080 (MSW_HTTP_PORT)
docker compose logs backend | grep setup_code   # the code that creates the admin, see Accounts below
```

The variables are prefixed `MSW_` because docker compose also reads the `.env` used for development.
`compose.env.sample` describes each of them.

## Configuration

* **`MSW_DOMAIN`** is the URL users open: the API refuses requests from any other origin. Its scheme sets the
  session cookie: `https://` makes it `Secure` (sent over HTTPS only, named `__Host-msw_session`), `http://`
  doesn't (a local network). A wrong scheme either never keeps the session (`https://` for an app served over HTTP)
  or lets it travel in clear (`http://` for an app served over HTTPS).
* **Images**: they are pulled from `MSW_REGISTRY` with the tag `MSW_TAG`, required: a version `X.Y.Z` (see
  [Upgrades](#upgrades)), or for a test a short commit sha or `latest`. Run `docker login` to the registry first
  (with Portainer: add the registry in its Registries). `docker-compose.yaml` never builds them, Portainer can't
  build. To build the images locally instead: `MSW_TAG=dev docker compose -f docker-compose.yaml -f
  docker-compose.build.yaml up --build`, the frontend one from `MSW_FRONTEND_PATH` (`../mysportweb-vuetify`).
* **Data**: in `MSW_DATA_DIR` (`./data` by default): `media/` holds the uploaded FIT files, thumbnails, pictures,
  elevation and map tiles (`dem/`, `tiles/`: downloaded again when lost), `postgres/` the bundled database. With
  Portainer, use absolute paths for `MSW_DATA_DIR` and `MSW_BACKUP_DIR`: the relative ones end up inside the stack
  directory of Portainer.
* **Bundled database**: `COMPOSE_PROFILES=db` in `.env` starts a Postgres, its data in `MSW_DATA_DIR/postgres`.
  Its user, `MSW_DBUSER`, is a superuser: at each start the backend uses it only to set up `<MSW_DBUSER>_app`
  (`mysportweb_app`), with the same password, without any privilege but owning the database and its tables, and
  works with that role. An SQL injection can then neither read the files of the server nor run commands. The
  superuser stays for the backups and `mysportweb admin`. A restore made as the superuser (see
  [Restore](#restore)) gives the tables back to `mysportweb_app` at the next start of the backend.
* **External database**: remove `COMPOSE_PROFILES` and set `MSW_DBHOST`, `MSW_DBPORT`, `MSW_DBUSER`,
  `MSW_DBPASSWORD`, `MSW_DBNAME`, and `MSW_DBSSLMODE`. The default, `prefer`, uses TLS when the database offers it
  without checking its certificate: set `verify-full` (or `require`) for a database reached over a network you
  don't control. The tables are created at the first start. Give the backend a user owning its database, not a
  superuser; given a superuser, it sets up `<MSW_DBUSER>_app` as above.
* **Registrations**: `MSW_REGISTRATION`, see [Accounts and administration](#accounts-and-administration).
* **Weather**: the weather at the start of each activity (temperature, feels like, wind, precipitation,
  humidity, clouds) comes from [Open-Meteo](https://open-meteo.com/), at the upload and, for the activities before,
  in the background at the start (the activities of a day at a place in one request, about 2400 an hour, 6000 per
  start). Only the start point rounded to 0.1° (about 10 km) and the date are sent: the start is often a home, and
  the weather of the past isn't more precise anyway. Open-Meteo is free for a non-commercial use (less than 10000
  requests a day), under CC BY 4.0: the activity page links to it. `MSW_WEATHER_URL=none` sends nothing; the URL
  of a [self-hosted Open-Meteo](https://github.com/open-meteo/open-meteo) asks it instead.
* **Elevation and map tiles**: `MSW_DEM_URL` and `MSW_MAP_TILE_URL` (AWS Terrain Tiles and OpenStreetMap by
  default), `none` to only use the tiles put in `MSW_DATA_DIR/media`.
* **Storage quota**: each user may store 1 GB of files (activities, thumbnails, pictures), `MSW_USER_QUOTA`
  (`500m`, `5g`, `none` for no quota). An upload above it is refused (507, with the space used). An admin gives a
  user another quota, or none, in the admin page; `GET /api/v1/user/storage` tells a user their space and quota.
* **Memory**: the backend is limited to 1 GB (`MSW_BACKEND_MEMORY`, `mem_limit` in `docker-compose.yaml`).
  Above it the kernel kills the backend, which restarts, rather than the database. The backend reads the limit at
  its start (logged: `memory limit of the container`): 2 uploads are analysed at a time under 2 GB, 4 from 2 GB.
  The largest file accepted (a GPX of 64 MB decompressed, 3 days recorded every second) takes about 350 MB.
* **Account imports**: the zip of an export a user imports is at most 2 GB (`MSW_IMPORT_MAX_SIZE`); a reverse
  proxy in front of the app must accept requests as large (`client_max_body_size` of nginx).
* **Content-Security-Policy**: enforced by default. If a page breaks (a map or a chart blank), set
  `MSW_CSP_HEADER=Content-Security-Policy-Report-Only` and open an issue with the report of the browser console.

## Accounts and administration

* **The first admin**: the first visitor of a new instance doesn't become its admin. While the instance has no
  admin, the backend logs a setup code at each start (`this instance has no admin yet ... setup_code=...`): the
  sign up page asks for it and creates the admin, then the code stops working.
* **Registrations**: `MSW_REGISTRATION` says who may register after:
  * `closed` (default): nobody, the admin can't create accounts yet either;
  * `invite`: whoever an admin invites, in **Admin** in the menu: an invitation registers one account, within 1
    to 90 days (7 by default), optionally only for one email. Its link, shown once, opens the sign up page with
    the code filled in. A used invitation stays in the list (who invited whom), an unused one can be revoked;
  * `open`: anyone reaching the app.
* **Without email**, the reset links below are given by hand; with the emails on (see [Emails](#emails)), the users
  ask for one from the login page.
* **Two-factor authentication**: required for the admins. An admin sets it up in **Your profile** (an
  authenticator app scans a QR code, and 10 single-use recovery codes are shown once). Until then, and with a
  session opened without it, the admin pages are refused. Any user can turn it on. The secrets are encrypted with
  `MSW_SECRET_KEY`: changing that key turns everyone's second factor unusable, reset it as below.
* **Managing the users**, in **Admin**: the list with each user's activities and disk use; making a user an admin
  or not; disabling an account (no login, its sessions and device tokens refused, its data kept) and enabling
  it again; deleting a disabled account with everything it owns (an account is disabled first, a slip can't
  delete it at once); a password reset link for a user, valid 24 hours, used once, given to them by hand (there
  is no email). Each of these asks a code of the admin's authenticator app, every time. Nobody can remove the
  last admin.
* **The activity of the users**, in **Admin**: the last login of each user; an event log (the logins and their
  failures, the uploads, the deletions, the exports and imports, the changes made by the admins), kept 90 days;
  and the notifications, with when they were queued and sent, to whom and about what, never their message.
* **Their data**, in **Your profile**: the export downloads a zip of everything the app keeps about the user (their
  uploaded files and every computed value as JSON, see its README.txt); deleting the account removes all of it,
  with the password, the username typed again, and the second factor code. The import takes the zip of an
  export, of this instance or another one, and adds what the account doesn't have: the activities (analysed
  again from their files, with the titles, equipment, visibility and trims set), the equipment with their
  pictures, services and reminders, the devices, the health measures, the segments, the goals, and the settings
  of the profile when asked for. Nothing of the account is replaced: importing the same zip twice adds nothing.
  It runs in the background (a notification when done).
* **From the command line**, when no admin can do it in the app:
  `docker compose exec backend mysportweb admin <command> <username>`, the commands being `promote` and `demote`
  (the roles; demoting the last admin brings the setup code back at the next start), `reset-password` (prints
  a reset link to give them) and `reset-2fa` (turns off the second factor of someone who
  lost their phone and their recovery codes).

## Backups and restore

### Backups

The `backup` service dumps the database every night (`MSW_BACKUP_TIME`, 03:30 by default) into
`MSW_BACKUP_DIR/db` (`./backups` by default). Each dump is checked before it counts, the dumps older than
`MSW_BACKUP_KEEP_DAYS` (14) are deleted once a new one succeeded, and the service is unhealthy when the last
backup is more than 26 hours old. A backup now, before an upgrade: `docker compose run --rm backup now`.

**The media files aren't in the dump**: send `MSW_BACKUP_DIR` and `MSW_DATA_DIR/media` elsewhere, a backup on the
disk of the server doesn't survive the disk.

* With a backup of the whole machine (Proxmox Backup, a snapshot of the VM or of the container), schedule it
  after `MSW_BACKUP_TIME` so that it holds the dump of the night; `MSW_DATA_DIR/postgres` is in it too, consistent
  as after a power cut (in a VM: qemu-guest-agent installed and enabled, the file systems are frozen during the
  snapshot).
* With a backup of files (restic, rsync, Kopia...), leave `MSW_DATA_DIR/postgres` out: its files copied one by
  one while the database runs aren't a database.

The dumps restore in any Postgres of the same major version or newer, without restoring the machine. With an
external database, `MSW_BACKUP_IMAGE` is a postgres image of its major version (pg_dump can't dump a more recent
server).

### Restore

From the directory of `docker-compose.yaml`. On the same server, the database replaced by a dump:

```sh
docker compose stop backend backup
docker compose exec -T db pg_restore -U mysportweb -d mysportweb --clean --if-exists --no-owner \
  < backups/db/mysportweb-2026-11-20_033000.dump
docker compose start backend backup
```

On a new server: the media restored into `MSW_DATA_DIR/media` (from the backup of the old one), then a new
database from the dump:

```sh
docker compose up -d db
# a new database starts twice (initialized, then for good): the restore waits for the second start
until docker compose logs db | grep -q "PostgreSQL init process complete"; do sleep 1; done
docker compose up -d --wait db
docker compose exec -T db pg_restore -U mysportweb -d mysportweb --clean --if-exists --no-owner \
  < backups/db/mysportweb-2026-11-20_033000.dump
docker compose up -d
```

The `init` service gives the media to the user of the backend (uid 1000) at each start. The backend refuses a
dump made by a more recent version of it (a newer database schema): restore it with that version. Try a restore
once, on another machine or database: an untested backup isn't a backup.

### Moving from the volumes

The versions before `MSW_DATA_DIR` kept the data in the `media` and `db` volumes: a backup with the old version,
the media copied out of its volume, the database restored from the dump.

```sh
# with the old docker-compose.yaml: a backup, then the stack stopped (stop, not down -v: the volumes stay)
docker compose run --rm backup now
docker compose stop
# the media into the new directory (docker volume ls: <project>_media, mysportweb_media by default)
docker run --rm -v mysportweb_media:/from:ro -v /srv/mysportweb/data/media:/to debian:trixie-slim \
  cp -a /from/. /to/
# with the new docker-compose.yaml and MSW_DATA_DIR=/srv/mysportweb/data: the database from the dump, as on a
# new server above (Portainer starts everything on an update: stop backend and backup before pg_restore)
docker compose up -d db
until docker compose logs db | grep -q "PostgreSQL init process complete"; do sleep 1; done
docker compose up -d --wait db
docker compose exec -T db pg_restore -U mysportweb -d mysportweb --clean --if-exists --no-owner \
  < backups/db/<the dump of the first step>.dump
docker compose up -d
# once the activities, thumbnails and pictures are checked: the volumes, and the copy of the media made by
# the old backup service
docker volume rm mysportweb_media mysportweb_db
rm -r backups/media
```

## Emails

The app sends emails to confirm the addresses of the users, for a forgotten password (**Forgot password?** on the
login page), the invitations (**Send by email** in Admin) and the notifications each user chooses by email. Without
email, everything works as before: the admins give the reset and invitation links by hand.

They go through the **SMTP relay of a sending service**: a mail server of your own gets its emails classified
as spam. Set in `.env`:

| Service  | `MSW_SMTP_HOST`          | `MSW_SMTP_PORT` | `MSW_SMTP_USERNAME`        | `MSW_SMTP_PASSWORD`         |
|----------|--------------------------|-----------------|----------------------------|-----------------------------|
| SendGrid | `smtp.sendgrid.net`      | 587             | `apikey` (that very word)  | an API key with Mail Send   |
| Postmark | `smtp.postmarkapp.com`   | 587             | the Server API token       | the same Server API token   |
| Mailgun  | `smtp.mailgun.org` (`smtp.eu.mailgun.org` in the EU) | 587 | the SMTP user of the domain | its SMTP password |
| Brevo    | `smtp-relay.brevo.com`   | 587             | the SMTP login of the account | an SMTP key              |
| Proton   | `smtp.protonmail.ch`     | 587             | an email address attached to your account | an SMTP key  |

and `MSW_MAIL_FROM`, the sender, e.g. `MySportWeb <sport@example.com>`. `MSW_SMTP_TLS` is `starttls` by default
(port 587), `tls` with port 465, `none` only towards a relay on the same machine. Check the current values in
the documentation of your service: they change seldom, but they do.

* **Deliverability**: verify the domain of `MSW_MAIL_FROM` at the service, which gives the DNS records to add:
  SPF and DKIM, and a DMARC record. Without them, even a relay lands in spam.
* **Checks**: an incomplete or inconsistent configuration (a host without port or sender, a username without
  password, an unknown `MSW_SMTP_TLS`, no encryption towards another machine...) stops the backend at its
  start, its log says what to fix. Whether the server answers isn't checked at the start: **Admin > Send a test
  email** sends one to you and shows the error of the server if any; the failures are counted in
  `mysportweb_emails_total` and the `MySportWebEmailsFailing` alert fires when none gets through.
* **Addresses**: a user confirms theirs from the email sent at the sign up (or from their profile, for the
  accounts created before the emails). Nothing is sent to an unconfirmed address but its confirmation. A new
  address replaces the old one once confirmed from its mailbox.

## Notifications

The app notifies a user when:

* maintenance reminders of an equipment are due (set on its page), once per cycle;
* a goal is reached, once per period of the goal;
* a recent activity holds new bests;
* someone comments one of their activities (at once), cheers them (grouped, once the cheers settle), or follows
  them. Each user turns these social notifications off in their profile.

Each user turns the notifications on in **Your profile → Notifications**, on each browser, with a test button; and
by email too, with the emails on and their address confirmed (see [Emails](#emails)).

* They are web push notifications: the app must be served over **HTTPS** (`MSW_DOMAIN`), and the backend must
  reach the push services of the browsers (Google for Chrome and the Chromium ones, Mozilla, Apple, Microsoft).
  It sends to those only.
* On iPhone and iPad, Safari only notifies an app added to the home screen (Share → Add to Home Screen).
* The VAPID keys signing them are generated at the first start and kept in the database: a restored database
  keeps them, and the subscriptions keep working.

## Monitoring

**Without Prometheus**: the admin page (Admin in the menu) starts with a **Status**: health checks (backups,
server errors, memory, disk, backfills, refused uploads, database connections, locked logins, emails, weather),
the current values, and charts of the last day. The backend reads its own metrics, a sample a minute kept in memory
for 24 hours: the charts start again at each restart. Prometheus and Grafana below keep the history and send alerts.

The backend serves metrics for Prometheus on its own port, 9090 in the container, never through the frontend.
`monitoring/` has what your Prometheus and Grafana need:

* `monitoring/prometheus/scrape.yml`: the scrape job, to add to `scrape_configs`. Keep its name, `mysportweb`.
* `monitoring/prometheus/alerts.yml`: the alert rules, to add to `rule_files`, with Alertmanager sending them:
  the backend down or restarting in a loop, **no successful backup for 26 hours**, 5xx errors, slow reads,
  uploads refused (busy), map tiles failing, a backfill failed, the database connection pool exhausted.
  `alerts_test.yml` tests them (`promtool test rules alerts_test.yml`, run by the CI).
* `monitoring/grafana/mysportweb.json`: the dashboard. In Grafana: Dashboards → New → Import, upload the file;
  choose the Prometheus at the top of the dashboard.

Where Prometheus reaches the metrics:

* **On the docker host**, by default: `127.0.0.1:9090` (`MSW_METRICS_ADDRESS`).
* **From another machine**: `MSW_METRICS_ADDRESS=0.0.0.0:9090`, and a token so that only Prometheus reads them:
  `MSW_METRICS_TOKEN=$(openssl rand -base64 32)` in the stack, the same token in the file of
  `credentials_file` in the scrape job. Restrict the port to the Prometheus server in the firewall too.
* **A Prometheus container in the network of the stack**: target `mysportweb-backend-1:9090` (`<stack>-backend-1`).

The metrics are named `mysportweb_*` (requests by route and status, analyses, thumbnails, map tiles, logins,
registrations, backfills and what they have left to do, segment matching, weather requests, users and activities,
backups), `go_sql_*` (the connection pool), `go_*` and `process_*`. The age of the backups comes from
`MSW_BACKUP_DIR`, mounted read-only in the backend: it reads it when `MSW_BACKUP_OWNER` is its user, `1000:1000` by
default (`mysportweb_backup_dir_readable` is 0 otherwise, and the backup alert can't fire). `METRICS_PORT=none` in
the environment of the backend turns the metrics off.

## Upgrades

The backend and the frontend are released together, with the same version: `MSW_TAG=X.Y.Z` deploys both images.
The backend logs its version at start, and `/api/v1/healthcheck` answers it.

Each version migrates the database schema at its start (versioned migrations, see
[internal/pkg/models/migrations/README.md](internal/pkg/models/migrations/README.md)), one server at a time, and an
older backend refuses to start on a schema migrated by a newer one. So, before each upgrade, take a dump:

```sh
docker compose run --rm backup now                   # a dump in MSW_BACKUP_DIR/db, checked
# with Portainer (no compose file on the host), through the backup container of the stack:
#   docker exec mysportweb-backup-1 bash -c 'pg_dump --format=custom --file=/backups/db/pre-upgrade-$(date +%F).dump'
# MSW_TAG=1.4.0 in .env (Portainer: in the environment of the stack), then
docker compose up -d
docker compose exec frontend curl -s localhost/api/v1/healthcheck   # {"status":"OK","version":"1.4.0"}
```

Pin `X.Y.Z`: `1.4` or `1` follow the next releases at each redeploy, migrations included.

A database created before the versioned migrations is adopted by the first version that has them: upgrade
through it, the later versions refuse such a database.

**Rollback**: put the previous `MSW_TAG` back and redeploy. If the backend then logs `the database schema is newer
than this version of the backend`, the new version migrated the database: restore the dump taken before the upgrade
(see [Restore](#restore)). The changes made since the upgrade are lost.

**Upgrade notes**:

* The version enforcing the Content-Security-Policy blocks what it doesn't allow, instead of only reporting it.
  Every page was checked without any report. If a page breaks (a map or a chart blank), set
  `MSW_CSP_HEADER=Content-Security-Policy-Report-Only` and open an issue with the report of the browser console.
* The version with the role `mysportweb_app` gives it the database and its tables at its first start (a few
  seconds), then works with it: nothing to do. Going back to an older version works too (it connects as the
  superuser, which can use anything).
* The version with `MSW_REGISTRATION` closes the registrations by default (they were open to anyone, the first
  user becoming the admin): set `MSW_REGISTRATION=open` to keep them open, or `invite`. The existing admin stays.

## Auditing an instance

`scripts/audit.sh` checks an instance from the outside, through its public URL: TLS, security headers, CORS and
CSRF, what must not be reachable (metrics, database, files), the authentication. Read only: it creates and
changes nothing, and attempts no login.

```sh
scripts/audit.sh https://sport.example.com                          # anonymous checks
MSW_AUDIT_TOKEN=msw_... scripts/audit.sh https://sport.example.com  # with a device token
```

With a device token, it checks what the token can and can't reach; with the session token of an admin logged in
with their second factor (the `__Host-msw_session` cookie), it also reads the admin status: backups, disk, the
server's health checks. The token goes in the environment, not on the command line (visible in `ps`). Exit
status: 0 all passed, 1 a warning, 2 a failure.
