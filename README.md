# MySportWeb

At first, it was just a little project to learn how to use Django for creating webapps with python

In 2023, during the Christmas holidays, I tried the Advent of Code and I challenged myself to learn Golang. I had a lot of fun and I learned a lot of things. 

So now, I'm rewriting the app in Golang with the Gin framework. I'm also trying to make it more modular and more scalable.

This repository hosts the backend of the application. Frontend is available here : https://github.com/Oupsman/mysportweb-vuetify

For now the app can :

* import and parse FIT files coming from various devices
* handle multiple users BUT the social side of the app is yet to be implemented
* handle multiple sports (running, biking, hiking, swimming)
* provide a ton of datas about the sessions (speed, distance, elevation, etc)
* Estimate your power level (cycling only for now)
* find your best efforts: the fastest 400 m to marathon of the runs, 10 to 100 km of the rides, and the best
  powers over 5 s to 1 h of any activity with a power meter (`GET /api/v1/user/records`)
* handle multiple equipments, but having a default equipment for each sport is still to be implemented

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
```

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
