package vars

import (
	"fmt"
	"github.com/joho/godotenv"
	"os"
	"strings"
)

// the version of the analysis engine, 2: time stamps of the sparse series, 3: the recording device,
// 4: the measured power of every sport and the time stamps of the temperatures, 5: the privacy zones
// around the first and the last GPS fixes (they were around the first and the last records),
// 6: the altitude correction of the device, the missing altitudes interpolated (they were 0) and the
// slope of the estimated power from them
const Engine_version int = 6

var ListenPort string

// DemDir is the directory of the elevation tiles of the altitude correction: DEM_DIR, MEDIA/dem by default
var DemDir string

// DemURL is where the missing elevation tiles are downloaded from: DEM_URL, elevation.DefaultURL by
// default, "none" for no download (the tiles are then only the ones put in DemDir)
var DemURL string
var Dsn string
var SecretKey string
var Domain string

// TrustedProxies are the addresses whose X-Forwarded-For gives the IP of the client (the rate limits
// count per IP): TRUSTED_PROXIES, comma separated IPs or CIDRs, by default the loopback and the private
// networks, where the nginx of the frontend runs
var TrustedProxies []string

// DefaultTrustedProxies are the loopback and private networks
var DefaultTrustedProxies = []string{"127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7"}

// MinSecretKeyLength is the shortest SECRET_KEY accepted, in bytes: the sessions are signed with HMAC-SHA256,
// a short key can be found from any token, then tokens of any user forged
const MinSecretKeyLength = 32

// CheckSecretKey refuses a SECRET_KEY too short to sign the sessions safely
func CheckSecretKey(key string) error {
	if len(key) < MinSecretKeyLength {
		return fmt.Errorf("SECRET_KEY must be at least %d bytes long, it is %d: generate one with "+
			"openssl rand -base64 48 (the users have to log in again)", MinSecretKeyLength, len(key))
	}
	return nil
}

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
	Dsn, err = DatabaseDSN(os.Getenv)
	if err != nil {
		panic(err.Error())
	}

	ListenPort = CheckVariable("LISTEN_PORT", true)
	SecretKey = CheckVariable("SECRET_KEY", true)
	if err := CheckSecretKey(SecretKey); err != nil {
		panic(err.Error())
	}
	Domain = CheckVariable("DOMAIN", true)
	DemDir = CheckVariable("DEM_DIR", false)
	if DemDir == "" {
		DemDir = "MEDIA/dem"
	}
	DemURL = CheckVariable("DEM_URL", false)
	TrustedProxies = DefaultTrustedProxies
	if proxies := CheckVariable("TRUSTED_PROXIES", false); proxies != "" {
		TrustedProxies = nil
		for _, proxy := range strings.Split(proxies, ",") {
			if proxy = strings.TrimSpace(proxy); proxy != "" {
				TrustedProxies = append(TrustedProxies, proxy)
			}
		}
	}
}
