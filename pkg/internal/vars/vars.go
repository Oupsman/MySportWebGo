package vars

import (
	"os"
)

var ListenPort string
var Dsn string
var SecretKey string

func CheckVariable(Key string, mandatory bool) string {
	if os.Getenv(Key) == "" && mandatory {
		panic("Missing environment variable: " + Key)
	}
	return os.Getenv(Key)
}

func Init() {
	Dsn = CheckVariable("DSN", true)
	ListenPort = CheckVariable("LISTEN_PORT", false)
	SecretKey = CheckVariable("SECRET_KEY", true)
}
