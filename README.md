# MySportWeb

At first, it was just a little project to learn how to use Django for creating webapps with python

Last year during the Christmas holidays, I tried the Advent of Code and I challenged myself to learn Golang. I had a lot of fun and I learned a lot of things. 

So now, I'm rewriting the app in Golang with the Gin framework. I'm also trying to make it more modular and more scalable.

This repository hosts the backend of the application. Frontend is available here : https://github.com/Oupsman/mysportweb-vuetify

For now the app can :

* import and parse FIT files coming from various devices
* handle multiple users BUT the social side of the app is yet to be implemented
* handle multiple sports (running, biking, hiking, swimming)
* provide a shitload of datas about the sessions (speed, distance, elevation, etc)
* Estimate your power level (cycling only for now)
* handle multiple equipments, but having a default equipment for each sport is still to be implemented

# For now, this app IS NOT PRODUCTION READY, AND VERY FAR FROM IT !

## TODO

1. Support more devices
2. Support more sports (skiing, snowboarding, etc)
3. Handle multiple languages
4. Support GPX & TCX files format 
5. Support multisessions FIT files 
6. Support laps and splits (I'm not using those)
