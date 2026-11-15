package vars

import (
	"fmt"
	"strings"
)

// DefaultSSLMode is the TLS mode of the database connection without DBSSLMODE: TLS when the server offers it,
// a plain connection otherwise (the bundled Postgres has no certificate). It doesn't check the server's
// certificate: an external database should be given require, verify-ca or verify-full.
const DefaultSSLMode = "prefer"

// SSLModes are the values of DBSSLMODE, those of libpq
var SSLModes = []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}

// DatabaseDSN returns the connection string of the database from the DB* variables of get (os.Getenv for
// the environment): DBHOST, DBPORT, DBUSER, DBPASSWORD, DBNAME, and DBSSLMODE (DefaultSSLMode when empty).
// The server and the command line tools build it here, so they connect the same way.
func DatabaseDSN(get func(string) string) (string, error) {
	var missing []string
	values := map[string]string{}
	for _, v := range []struct{ key, variable string }{
		{"host", "DBHOST"}, {"port", "DBPORT"}, {"user", "DBUSER"}, {"password", "DBPASSWORD"}, {"dbname", "DBNAME"},
	} {
		values[v.key] = get(v.variable)
		if values[v.key] == "" {
			missing = append(missing, v.variable)
		}
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("missing database variables %s: set them in the environment or in .env",
			strings.Join(missing, ", "))
	}
	sslmode := get("DBSSLMODE")
	if sslmode == "" {
		sslmode = DefaultSSLMode
	}
	valid := false
	for _, mode := range SSLModes {
		valid = valid || sslmode == mode
	}
	if !valid {
		return "", fmt.Errorf("DBSSLMODE %q is not one of %s", sslmode, strings.Join(SSLModes, ", "))
	}
	values["sslmode"] = sslmode

	var dsn []string
	for _, key := range []string{"host", "port", "user", "password", "dbname", "sslmode"} {
		dsn = append(dsn, key+"="+quoteDSNValue(values[key]))
	}
	return strings.Join(dsn, " "), nil
}

// quoteDSNValue quotes a value of a keyword/value connection string as libpq reads it: between single
// quotes, with the quotes and backslashes escaped. A password with a space or a quote would otherwise
// cut the string, or set another keyword.
func quoteDSNValue(value string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value) + "'"
}
