# MySportWeb

At first, it was just a little project to learn how to use Django for creating webapps with python

In 2023, during the Christmas holidays, I tried the Advent of Code and I challenged myself to learn Golang. I had a lot of fun and I learned a lot of things. 

So now, I'm rewriting the app in Golang with the Gin framework. I'm also trying to make it more modular and more scalable.

This repository hosts the backend of the application. Frontend is available here : https://github.com/Oupsman/mysportweb-vuetify

For now the app can :

* import and parse FIT files coming from various devices, and GPX and TCX files (gzipped or not): they are
  converted to FIT files, the sport of a GPX file that doesn't name it chosen at the upload. The GPX or TCX file
  is kept beside the FIT file, never served
* handle multiple users: they find each other by their username and follow each other (each follower accepted
  first, unless the user turns it off); a feed shows the activities of the users followed that aren't private.
  They cheer the activities of the others they may see, and comment those whose owner allows comments ("Comments
  allowed", off by default); the owner is notified, and deletes any comment on their activities
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
* export all your data as a zip, import it into another account or instance, or delete your account
* find your best efforts: the fastest 400 m to marathon of the runs, 10 to 100 km of the rides, and the best
  powers over 5 s to 1 h of any activity with a power meter (`GET /api/v1/user/records`)
* handle multiple equipments, with a default equipment for each sport (`default_sports` of an equipment, the
  choices from `GET /api/v1/user/sports`): an imported activity gets the one of its sport, a commute ride the
  commute bike. The activities imported before keep theirs

## Status

MySportWeb is self-hosted, used daily by its author on a small instance, and not audited by anyone else. It is built
to be exposed to the internet, for a few users who know each other: a family, a club.

What protects the users and their data:

* **Accounts**: registrations closed by default (`MSW_REGISTRATION`: closed, by invitation, or open). Passwords of
  10 characters at least, the common ones refused, hashed with bcrypt. Two-factor authentication (an authenticator
  app, recovery codes) for everyone, required for the admins, and asked again for each change of a user.
* **Sessions**: in an HttpOnly cookie a script can't read, refused from other sites (CSRF), ended by the logout, a
  password change or a disabled account. The desktop application and the scripts use device tokens, limited to
  what they are given.
* **Logins**: limited per IP; wrong passwords lock out the IP they come from, not the owner of the account, and
  guesses spread over many IPs lock out the new browsers only, never the ones its owner used.
* **The pages**: a Content-Security-Policy, HSTS, no framing by other sites. The traces near the homes of the users
  (their privacy distance) are cut from what the others see.
* **The server**: uploads analysed a few at a time, within a memory limit (`MSW_BACKEND_MEMORY`) that protects the
  database; a storage quota per user (`MSW_USER_QUOTA`); the size of the requests and of the decompressed files
  bounded. The backend works with a database role without privileges, and runs as an unprivileged user.
* **The data**: a nightly dump of the database, checked, with an alert when it is too old; each user exports all
  their data, imports it into another account or instance, or deletes their account. Metrics and alerts for Prometheus and Grafana (see Monitoring).

What it lacks:

* **One instance only**: the login limits and the analysis slots are in the memory of the backend, nothing shares
  them between two backends.
* **No visibility for the followers only**: an activity is private, for every user of the instance, or public. The
  logged in users find each other by their username: every user of the instance can find every other one.
* **English only**.
* **The media files aren't in the nightly dump**: back up `MSW_DATA_DIR/media` with the machine (see
  [Backups](INSTALL.md#backups) in INSTALL.md).

## Desktop synchronization

`desktop/` is a desktop application uploading the activities of the devices plugged through USB (mass storage or MTP)
with a device token, see [desktop/README.md](desktop/README.md).

## Installation

MySportWeb runs with Docker Compose: see [INSTALL.md](INSTALL.md) for the deployment, the configuration, the
administration, the backups, the emails, the monitoring and the upgrades. In short:

```sh
cp compose.env.sample .env   # then set MSW_TAG, MSW_DOMAIN, MSW_SECRET_KEY and MSW_DBPASSWORD
docker compose up -d
docker compose logs backend | grep setup_code   # the code that creates the admin
```

## Releases

The backend and the frontend are released together, with the same version: `vX.Y.Z` is a tag in both repositories,
even when one of them didn't change, and `MSW_TAG=X.Y.Z` deploys both images. From the default branch of both
repositories, committed and pushed:

```sh
scripts/release.sh 1.4.0   # checks both repositories, asks, tags both v1.4.0 and pushes the tags
```

The CI of each repository tests the tagged commit and publishes its image `1.4.0` (and `1.4`, `1`). Deploying it:
see [Upgrades](INSTALL.md#upgrades) in INSTALL.md.

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
