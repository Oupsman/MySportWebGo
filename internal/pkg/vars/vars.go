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
// slope of the estimated power from them, 7: the running dynamics in the metrics
const Engine_version int = 7

// Version is the version of the backend: X.Y.Z for a release, the short commit sha for an image of the
// default branch, dev for a local build. Set at the build (-ldflags -X, see the Dockerfile).
var Version = "dev"

var ListenPort string

// MetricsPort is the port of the metrics listener (Prometheus): METRICS_PORT, 9090 by default, "none" for no
// metrics. Never served by the frontend: publish it to the Prometheus server only.
var MetricsPort string

// MetricsToken, when set, is required from the scrapes: "Authorization: Bearer <METRICS_TOKEN>"
var MetricsToken string

// BackupDir is where the backup service writes (mounted read-only): BACKUP_DIR, "" without; the metrics then
// tell the age of the last backup
var BackupDir string

// DemDir is the directory of the elevation tiles of the altitude correction: DEM_DIR, MEDIA/dem by default
var DemDir string

// DemURL is where the missing elevation tiles are downloaded from: DEM_URL, elevation.DefaultURL by
// default, "none" for no download (the tiles are then only the ones put in DemDir)
var DemURL string

// MapTileDir is the directory of the map tiles of the thumbnails: MAP_TILE_DIR, MEDIA/tiles by default
var MapTileDir string

// MapTileURL is where the map tiles of the thumbnails are downloaded from: MAP_TILE_URL,
// staticmap.DefaultURL (OpenStreetMap) by default, "none" for no download (the tiles are then only the ones
// already in MapTileDir)
var MapTileURL string

// WeatherURL is where the weather of the activities comes from: WEATHER_URL, the public servers of Open-Meteo
// when empty, none for no weather, else a server of the same API (a self-hosted Open-Meteo)
var WeatherURL string
var Dsn string
var SecretKey string
var Domain string

// Registration says who may create an account: REGISTRATION, closed by default
var Registration RegistrationMode

// RegistrationMode is who may create an account
type RegistrationMode string

const (
	// RegistrationClosed: nobody, except the admin of a new instance with the setup code of the logs
	RegistrationClosed RegistrationMode = "closed"
	// RegistrationInvite: the holders of an invitation created by an admin
	RegistrationInvite RegistrationMode = "invite"
	// RegistrationOpen: anyone reaching the instance
	RegistrationOpen RegistrationMode = "open"
)

// ParseRegistration reads REGISTRATION: closed when empty, an error for an unknown value (a typo must not
// open the registrations nor close them silently)
func ParseRegistration(value string) (RegistrationMode, error) {
	switch mode := RegistrationMode(strings.ToLower(strings.TrimSpace(value))); mode {
	case "":
		return RegistrationClosed, nil
	case RegistrationClosed, RegistrationInvite, RegistrationOpen:
		return mode, nil
	}
	return "", fmt.Errorf("REGISTRATION must be closed, invite or open, it is %q", value)
}

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
	MetricsPort = CheckVariable("METRICS_PORT", false)
	if MetricsPort == "" {
		MetricsPort = "9090"
	}
	MetricsToken = CheckVariable("METRICS_TOKEN", false)
	BackupDir = CheckVariable("BACKUP_DIR", false)
	SecretKey = CheckVariable("SECRET_KEY", true)
	if err := CheckSecretKey(SecretKey); err != nil {
		panic(err.Error())
	}
	Domain = CheckVariable("DOMAIN", true)
	Registration, err = ParseRegistration(CheckVariable("REGISTRATION", false))
	if err != nil {
		panic(err.Error())
	}
	DemDir = CheckVariable("DEM_DIR", false)
	if DemDir == "" {
		DemDir = "MEDIA/dem"
	}
	DemURL = CheckVariable("DEM_URL", false)
	if quota := CheckVariable("USER_QUOTA", false); quota != "" {
		if UserQuota, err = ParseSize(quota); err != nil {
			panic("USER_QUOTA: " + err.Error())
		}
	}
	MapTileDir = CheckVariable("MAP_TILE_DIR", false)
	if MapTileDir == "" {
		MapTileDir = "MEDIA/tiles"
	}
	MapTileURL = CheckVariable("MAP_TILE_URL", false)
	WeatherURL = CheckVariable("WEATHER_URL", false)
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
