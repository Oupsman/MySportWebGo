package vars

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func database(password, sslmode string) map[string]string {
	return map[string]string{"DBHOST": "db.example.com", "DBPORT": "5433", "DBUSER": "msw user",
		"DBPASSWORD": password, "DBNAME": "mysportweb", "DBSSLMODE": sslmode}
}

// the connection string is read back by the driver as it was meant: every password, whatever its characters
func TestDatabaseDSN(t *testing.T) {
	for _, password := range []string{
		"simple",
		"with spaces",
		"it's",
		`back\slash`,
		`trailing\`,
		"sslmode=disable",                // was read as another keyword
		"x dbname=other sslmode=disable", // idem, with a space
		"pa=ss'wo\\rd \"quoted\" ",
	} {
		dsn, err := DatabaseDSN(env(database(password, "disable")))
		if err != nil {
			t.Fatal(err)
		}
		config, err := pgconn.ParseConfig(dsn)
		if err != nil {
			t.Errorf("%q: %v", password, err)
			continue
		}
		if config.Password != password || config.User != "msw user" || config.Database != "mysportweb" ||
			config.Host != "db.example.com" || config.Port != 5433 || config.TLSConfig != nil {
			t.Errorf("%q: read back password %q user %q database %q host %s:%d tls %v", password, config.Password,
				config.User, config.Database, config.Host, config.Port, config.TLSConfig != nil)
		}
	}
}

func TestDatabaseDSNSSLMode(t *testing.T) {
	// prefer by default: TLS first, then a plain connection
	dsn, err := DatabaseDSN(env(database("p", "")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dsn, "sslmode='prefer'") {
		t.Errorf("default: %s", dsn)
	}
	config, err := pgconn.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if config.TLSConfig == nil || len(config.Fallbacks) != 1 || config.Fallbacks[0].TLSConfig != nil {
		t.Errorf("prefer: TLS %v, fallbacks %d", config.TLSConfig != nil, len(config.Fallbacks))
	}

	for _, mode := range SSLModes {
		if _, err := DatabaseDSN(env(database("p", mode))); err != nil {
			t.Errorf("%s: %v", mode, err)
		}
	}
	if _, err := DatabaseDSN(env(database("p", "on"))); err == nil {
		t.Error("an unknown sslmode accepted")
	}
}

func TestDatabaseDSNMissing(t *testing.T) {
	values := database("p", "")
	delete(values, "DBHOST")
	values["DBPASSWORD"] = ""
	_, err := DatabaseDSN(env(values))
	if err == nil || !strings.Contains(err.Error(), "DBHOST, DBPASSWORD") {
		t.Errorf("missing variables: %v", err)
	}
}
