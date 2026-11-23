# MySportWeb

At first, it was just a little project to learn how to use Django for creating webapps with python

In 2023, during the Christmas holidays, I tried the Advent of Code and I challenged myself to learn Golang. I had a lot of fun and I learned a lot of things. 

So now, I'm rewriting the app in Golang with the Gin framework. I'm also trying to make it more modular and more scalable.

This repository hosts the backend of the application. Frontend is available here : https://github.com/Oupsman/mysportweb-vuetify

For now the app can :

* import and parse FIT files coming from various devices, and GPX and TCX files (gzipped or not): they are
  converted to FIT files, the sport of a GPX file that doesn't name it chosen at the upload. The GPX or TCX file
  is kept beside the FIT file, never served
* handle multiple users BUT the social side of the app is yet to be implemented
* handle multiple sports (running, biking, hiking, swimming)
* provide a ton of datas about the sessions (speed, distance, elevation, etc)
* Estimate your power level (cycling only for now)
* power zones (Coggan's 7), normalized power, intensity factor and TSS of the rides with a power meter, by your
  FTP: yours when you set it in your profile, else 95% of your best 20 minutes of the 42 days before the ride.
  A ride's training score is the one its device recorded, else its TSS, else the TRIMP of its heart rate
* your heatmap (**Heatmap** in the menu): every trace of your activities on one map, brighter where you go
  often, by period and sport, a click listing the activities passing there; the starts and ends can be hidden
  (your privacy distance) for screenshots
* maintenance reminders, several per equipment: every so many km, hours of activities, or days, weeks,
  months or years (calendar ones), whichever comes first, e.g. change the chain every 3000 km, clean the
  transmission every month, check the tire pressure every week. A time one waits for a ride since it was
  last done, unless told otherwise. Marking one done logs a service; a service can do several. Notified once
  per cycle, grouped per equipment, the time ones from 08:00 in your timezone
* the power-duration curve: your best power over each duration from 1 s to 5 h, of a period or compared with
  another (`GET /api/v1/user/powercurve?from=&to=`), in W or W/kg (the weight at the date of each ride), with
  the critical power fitted on its 3 to 20 minutes
* find your best efforts: the fastest 400 m to marathon of the runs, 10 to 100 km of the rides, and the best
  powers over 5 s to 1 h of any activity with a power meter (`GET /api/v1/user/records`)
* handle multiple equipments, with a default equipment for each sport (`default_sports` of an equipment, the
  choices from `GET /api/v1/user/sports`): an imported activity gets the one of its sport, a commute ride the
  commute bike. The activities imported before keep theirs

# For now, this app IS NOT PRODUCTION READY, AND VERY FAR FROM IT !

## Desktop synchronization

`desktop/` is a desktop application uploading the activities of the devices plugged through USB (mass storage or MTP)
with a device token, see [desktop/README.md](desktop/README.md).

## Deploy with Docker Compose

`docker-compose.yaml` runs the frontend (nginx, which forwards `/api` and `/MEDIA` to the backend) and the backend,
with an optional Postgres:

```sh
cp compose.env.sample .env   # then set MSW_TAG, MSW_DOMAIN, MSW_SECRET_KEY and MSW_DBPASSWORD
docker compose up -d         # the app is on http://localhost:8080 (MSW_HTTP_PORT)
docker compose logs backend | grep setup_code   # the code that creates the admin, see Accounts below
```

* **Accounts**: the first visitor of a new instance doesn't become its admin. While the instance has no admin, the
  backend logs a setup code at each start (`this instance has no admin yet ... setup_code=...`): the sign up page
  asks for it and creates the admin, then the code stops working. `MSW_REGISTRATION` says who may register after:
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
* **Your data**, in **Your profile**: the export downloads a zip of everything the app keeps about you (your
  uploaded files and every computed value as JSON, see its README.txt); deleting your account removes all of it,
  with your password, your username typed again, and your second factor code.
* **From the command line**, when no admin can do it in the app:
  `docker compose exec backend mysportweb admin <command> <username>`, the commands being `promote` and `demote`
  (the roles; demoting the last admin brings the setup code back at the next start), `reset-password` (prints
  a reset link to give them) and `reset-2fa` (turns off the second factor of someone who
  lost their phone and their recovery codes).

* **Data**: in `MSW_DATA_DIR` (`./data` by default): `media/` holds the uploaded FIT files, thumbnails, pictures,
  elevation and map tiles (`dem/`, `tiles/`: downloaded again when lost), `postgres/` the bundled database. With
  Portainer, use absolute paths for `MSW_DATA_DIR` and `MSW_BACKUP_DIR`: the relative ones end up inside the stack
  directory of Portainer.
* **Bundled database**: `COMPOSE_PROFILES=db` in `.env` starts a Postgres, its data in `MSW_DATA_DIR/postgres`.
* **External database**: remove `COMPOSE_PROFILES` and set `MSW_DBHOST`, `MSW_DBPORT`, `MSW_DBUSER`, `MSW_DBPASSWORD`,
  `MSW_DBNAME`, and `MSW_DBSSLMODE`. The default, `prefer`, uses TLS when the database offers it without checking
  its certificate: set `verify-full` (or `require`) for a database reached over a network you don't control.
  The tables are created at the first start.
* **Upgrades**: each version migrates the database schema at its start (versioned migrations, see
  [internal/pkg/models/migrations/README.md](internal/pkg/models/migrations/README.md)), one server at a time. Back
  up the database before upgrading: the backend refuses to start on a database migrated by a newer version, going
  back to an older one needs that backup. A database created before the versioned migrations is adopted by the
  first version that has them: upgrade through it, the later versions refuse such a database.
* `MSW_DOMAIN` is the URL users open: the API refuses requests from any other origin. Its scheme sets the session
  cookie: `https://` makes it `Secure` (sent over HTTPS only, named `__Host-msw_session`), `http://` doesn't (a
  local network). A wrong scheme either never keeps the session (`https://` for an app served over HTTP) or lets
  it travel in clear (`http://` for an app served over HTTPS).
* **Backups**: the `backup` service dumps the database every night (`MSW_BACKUP_TIME`, 03:30 by default) into
  `MSW_BACKUP_DIR/db` (`./backups` by default). Each dump is checked before it counts, the dumps older than
  `MSW_BACKUP_KEEP_DAYS` (14) are deleted once a new one succeeded, and the service is unhealthy when the last
  backup is more than 26 hours old. A backup now, before an upgrade: `docker compose run --rm backup now`.
  Send `MSW_BACKUP_DIR` and `MSW_DATA_DIR/media` elsewhere: a backup on the disk of the server doesn't survive the
  disk. With a backup of the whole machine (Proxmox Backup, a snapshot of the VM or of the container), schedule it
  after `MSW_BACKUP_TIME` so that it holds the dump of the night; `MSW_DATA_DIR/postgres` is in it too, consistent
  as after a power cut (in a VM: qemu-guest-agent installed and enabled, the file systems are frozen during the
  snapshot). With a backup of files (restic, rsync, Kopia...), leave `MSW_DATA_DIR/postgres` out: its files copied
  one by one while the database runs aren't a database. The dumps restore in any Postgres of the same major
  version or newer, without restoring the machine. With an external database, `MSW_BACKUP_IMAGE` is a postgres
  image of its major version (pg_dump can't dump a more recent server).
* **Restore**, from the directory of `docker-compose.yaml`. On the same server, the database replaced by a dump:

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
* **Moving from the volumes** (the versions before `MSW_DATA_DIR` kept the data in the `media` and `db` volumes):
  a backup with the old version, the media copied out of its volume, the database restored from the dump.

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

* The images are pulled from `MSW_REGISTRY` with the tag `MSW_TAG`, required: a version `X.Y.Z` (see
  [Releases and upgrades](#releases-and-upgrades)), or for a test a short commit sha or `latest`. Run `docker login`
  to the registry first (with Portainer: add the registry in its Registries). `docker-compose.yaml` never builds
  them, Portainer can't build.
* To build the images locally instead: `MSW_TAG=dev docker compose -f docker-compose.yaml -f docker-compose.build.yaml
  up --build`, the frontend one from `MSW_FRONTEND_PATH` (`../mysportweb-vuetify`).

The variables are prefixed `MSW_` because docker compose also reads the `.env` used for development.

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

The app notifies the browsers of a user when maintenance reminders of an equipment are due (set on its page),
once per cycle. Each user turns them on in **Your profile → Notifications**, on each browser, with a test button;
and by email too, with the emails on and their address confirmed (see [Emails](#emails)).

* They are web push notifications: the app must be served over **HTTPS** (`MSW_DOMAIN`), and the backend must
  reach the push services of the browsers (Google for Chrome and the Chromium ones, Mozilla, Apple, Microsoft).
  It sends to those only.
* On iPhone and iPad, Safari only notifies an app added to the home screen (Share → Add to Home Screen).
* The VAPID keys signing them are generated at the first start and kept in the database: a restored database
  keeps them, and the subscriptions keep working.

## Monitoring

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
registrations,
backfills and what they have left to do, segment matching, users and activities, backups), `go_sql_*` (the connection pool), `go_*` and `process_*`. The age
of the backups comes from `MSW_BACKUP_DIR`, mounted read-only in the backend: it reads it when
`MSW_BACKUP_OWNER` is its user, `1000:1000` by default (`mysportweb_backup_dir_readable` is 0 otherwise, and
the backup alert can't fire). `METRICS_PORT=none` in the environment of the backend turns the metrics off.

## Releases and upgrades

The backend and the frontend are released together, with the same version: `vX.Y.Z` is a tag in both repositories,
even when one of them didn't change, and `MSW_TAG=X.Y.Z` deploys both images.

**Release**: from the default branch of both repositories, committed and pushed:

```sh
scripts/release.sh 1.4.0   # checks both repositories, asks, tags both v1.4.0 and pushes the tags
```

The CI of each repository tests the tagged commit and publishes its image `1.4.0` (and `1.4`, `1`). The backend
logs its version at start, and `/api/v1/healthcheck` answers it.

**Upgrade**: a new version may migrate the database schema at its start, and an older backend refuses to start on
a schema migrated by a newer one. So, before each upgrade, take a dump:

```sh
docker compose run --rm backup now                   # a dump in MSW_BACKUP_DIR/db, checked
# with Portainer (no compose file on the host), through the backup container of the stack:
#   docker exec mysportweb-backup-1 bash -c 'pg_dump --format=custom --file=/backups/db/pre-upgrade-$(date +%F).dump'
# MSW_TAG=1.4.0 in .env (Portainer: in the environment of the stack), then
docker compose up -d
docker compose exec frontend curl -s localhost/api/v1/healthcheck   # {"status":"OK","version":"1.4.0"}
```

**Rollback**: put the previous `MSW_TAG` back and redeploy. If the backend then logs `the database schema is newer
than this version of the backend`, the new version migrated the database: restore the dump taken before the upgrade
(see **Restore** in [Deploy with Docker Compose](#deploy-with-docker-compose)). The changes made since the upgrade
are lost.

**Upgrade notes**:

* The version with `MSW_REGISTRATION` closes the registrations by default (they were open to anyone, the first
  user becoming the admin): set `MSW_REGISTRATION=open` to keep them open, or `invite`. The existing admin stays.

Pin `X.Y.Z`: `1.4` or `1` follow the next releases at each redeploy, migrations included.

## CI with Woodpecker

`.woodpecker.yaml` (the frontend repository has its own) tests every push and pull request, then builds the image.
The default branch pushes `latest` and the short commit sha; a version tag `v1.2.3` pushes `1.2.3`, `1.2` and `1`
(`ci/image-tags.sh`, which also writes the version built into the backend). Pull requests and other branches build the image without pushing it.

* Secrets of each repository: `docker_registry` (the host, e.g. `registry.example.com`), `docker_repo` (the image,
  e.g. `registry.example.com/oupsman/mysportwebgo`), `docker_username`, `docker_password`.
* The server must allow the build plugin to run privileged:
  `WOODPECKER_PLUGINS_PRIVILEGED=docker.io/woodpeckerci/plugin-docker-buildx:6.1.2` (the exact image).
* A registry without TLS or with a self-signed certificate needs the `-insecure` variant of the plugin and
  `insecure: true` in its settings, or its CA certificate mounted in the agent (see the plugin documentation).

## TODO

1. Support more devices 
2. support Healthdatas
2.1 Integrate with Withings API - Oauth2
3. ~~Support more sports (skiing, snowboarding, etc)~~ (every sport of the FIT profile)
4. Handle multiple languages
5. Support GPX & TCX files format 
6. ~~Support multisessions FIT files~~ (one activity with legs)
7. Support laps and splits (I'm not using those)
8. Plugin system

## Credits

City names come from [GeoNames](https://www.geonames.org/) (cities with more than 15000 inhabitants), licensed under [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/).
