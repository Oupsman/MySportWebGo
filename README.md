# MySportWeb

At first, it was just a little project to learn how to use Django for creating webapps with python

In 2023 during the Christmas holidays, I tried the Advent of Code and I challenged myself to learn Golang. I had a lot of fun and I learned a lot of things. 

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
cp compose.env.sample .env   # then set MSW_DOMAIN, MSW_SECRET_KEY and MSW_DBPASSWORD
docker compose up -d         # the app is on http://localhost:8080 (MSW_HTTP_PORT)
```

* **Bundled database**: `COMPOSE_PROFILES=db` in `.env` starts a Postgres, its data in the `db` volume.
* **External database**: remove `COMPOSE_PROFILES` and set `MSW_DBHOST`, `MSW_DBPORT`, `MSW_DBUSER`, `MSW_DBPASSWORD`,
  `MSW_DBNAME`, and `MSW_DBSSLMODE`. The default, `prefer`, uses TLS when the database offers it without checking
  its certificate: set `verify-full` (or `require`) for a database reached over a network you don't control.
  The tables are created at the first start.
* **Upgrades**: each version migrates the database schema at its start (versioned migrations, see
  [internal/pkg/models/migrations/README.md](internal/pkg/models/migrations/README.md)), one server at a time. Back
  up the database before upgrading: the backend refuses to start on a database migrated by a newer version, going
  back to an older one needs that backup. A database created before the versioned migrations is adopted by the
  first version that has them: upgrade through it, the later versions refuse such a database.
* `MSW_DOMAIN` is the URL users open: the API refuses requests from any other origin.
* The uploaded files are in the `media` volume.
* **Backups**: the `backup` service dumps the database every night (`MSW_BACKUP_TIME`, 03:30 by default) into
  `MSW_BACKUP_DIR/db` (`./backups` by default), and copies the media volume into `MSW_BACKUP_DIR/media`. Send that
  directory elsewhere (Kopia, restic...): a backup on the disk of the server doesn't survive the disk. Each dump is
  checked before it counts, the dumps older than `MSW_BACKUP_KEEP_DAYS` (14) are deleted once a new one succeeded,
  and the service is unhealthy when the last backup is more than 26 hours old. The media copy is incremental: the
  files deleted since stay in it. A backup now, before an upgrade: `docker compose run --rm backup now`.
  With an external database, `MSW_BACKUP_IMAGE` is a postgres image of its major version (pg_dump can't dump a
  more recent server).
* **Restore** (the backend stopped, from the directory of `docker-compose.yaml`):

  ```sh
  docker compose stop backend
  # the database: replaces its tables by the ones of the dump
  docker compose exec -T db pg_restore -U mysportweb -d mysportweb --clean --if-exists --no-owner \
    < backups/db/mysportweb-2026-11-20_033000.dump
  # the media: the files of the copy, owned by the user of the backend
  docker run --rm -v mysportweb_media:/media -v ./backups/media:/copy:ro debian:bookworm-slim \
    sh -c 'cp -a /copy/. /media/ && chown -R 1000:1000 /media'
  docker compose start backend
  ```

  The backend refuses a dump made by a more recent version of it (a newer database schema): restore it with that
  version. Try a restore once, on another machine or database: an untested backup isn't a backup.
* The images are pulled from `MSW_REGISTRY` with the tag `MSW_TAG` (`latest`, a short commit sha, or a version
  `1.2.3`, `1.2`, `1`), at each deployment: run `docker login` to the registry first (with Portainer: add the
  registry in its Registries). `docker-compose.yaml` never builds them, Portainer can't build.
* To build the images locally instead: `docker compose -f docker-compose.yaml -f docker-compose.build.yaml up --build`,
  the frontend one from `MSW_FRONTEND_PATH` (`../mysportweb-vuetify`).

The variables are prefixed `MSW_` because docker compose also reads the `.env` used for development.

## CI with Woodpecker

`.woodpecker.yaml` (the frontend repository has its own) tests every push and pull request, then builds the image.
The default branch pushes `latest` and the short commit sha; a version tag `v1.2.3` pushes `1.2.3`, `1.2` and `1`
(`ci/image-tags.sh`). Pull requests and other branches build the image without pushing it.

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
