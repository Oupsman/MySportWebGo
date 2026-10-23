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
* handle multiple equipments, but having a default equipment for each sport is still to be implemented

# For now, this app IS NOT PRODUCTION READY, AND VERY FAR FROM IT !

## Deploy with Docker Compose

`docker-compose.yaml` runs the frontend (nginx, which forwards `/api` and `/MEDIA` to the backend) and the backend,
with an optional Postgres:

```sh
cp compose.env.sample .env   # then set MSW_DOMAIN, MSW_SECRET_KEY and MSW_DBPASSWORD
docker compose up -d         # the app is on http://localhost:8080 (MSW_HTTP_PORT)
```

* **Bundled database**: `COMPOSE_PROFILES=db` in `.env` starts a Postgres, its data in the `db` volume.
* **External database**: remove `COMPOSE_PROFILES` and set `MSW_DBHOST`, `MSW_DBPORT`, `MSW_DBUSER`, `MSW_DBPASSWORD`,
  `MSW_DBNAME`, and `MSW_DBSSLMODE` (`require`...) when it needs TLS. The tables are created at the first start.
* `MSW_DOMAIN` is the URL users open: the API refuses requests from any other origin.
* The uploaded files are in the `media` volume.
* `docker compose build` builds both images, the frontend one from `MSW_FRONTEND_PATH` (`../mysportweb-vuetify`);
  otherwise they are pulled from `MSW_REGISTRY` (`ghcr.io/oupsman`) with the tag `MSW_TAG` (`latest`).

The variables are prefixed `MSW_` because docker compose also reads the `.env` used for development.

## TODO

1. Support more devices 
2. support Healthdatas
2.1 Integrate with Withings API - Oauth2
3. Support more sports (skiing, snowboarding, etc)
4. Handle multiple languages
5. Support GPX & TCX files format 
6. ~~Support multisessions FIT files~~ (one activity with legs)
7. Support laps and splits (I'm not using those)
8. Plugin system

## Credits

City names come from [GeoNames](https://www.geonames.org/) (cities with more than 15000 inhabitants), licensed under [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/).
