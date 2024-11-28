package vars

import (
	"fmt"
	"github.com/joho/godotenv"
	"os"
)

const Engine_version int = 1 // the version of the analysis engine

var ListenPort string
var Dsn string
var SecretKey string
var Domain string

func CheckVariable(Key string, mandatory bool) string {
	if os.Getenv(Key) == "" && mandatory {
		panic("Missing environment variable: " + Key)
	}
	return os.Getenv(Key)
}

func Init() {
	err := godotenv.Load()
	if err != nil {
		fmt.Println("Error loading .env file")
	}
	dbhost := CheckVariable("DBHOST", true)
	dbport := CheckVariable("DBPORT", true)
	dbuser := CheckVariable("DBUSER", true)
	dbpass := CheckVariable("DBPASSWORD", true)
	dbname := CheckVariable("DBNAME", true)

	Dsn = fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable", dbhost, dbuser, dbpass, dbname, dbport)
	Domain = CheckVariable("DOMAIN", true)
	ListenPort = CheckVariable("LISTEN_PORT", true)
	SecretKey = CheckVariable("SECRET_KEY", true)
}
