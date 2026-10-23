package vars

import (
	"fmt"
	"github.com/joho/godotenv"
	"os"
)

// the version of the analysis engine, 2: time stamps of the sparse series, 3: the recording device
const Engine_version int = 3

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
	// an external database often requires TLS: require, verify-ca or verify-full
	sslmode := CheckVariable("DBSSLMODE", false)
	if sslmode == "" {
		sslmode = "disable"
	}

	Dsn = fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=%s", dbhost, dbuser, dbpass, dbname, dbport, sslmode)

	ListenPort = CheckVariable("LISTEN_PORT", true)
	SecretKey = CheckVariable("SECRET_KEY", true)
	Domain = CheckVariable("DOMAIN", true)
}
