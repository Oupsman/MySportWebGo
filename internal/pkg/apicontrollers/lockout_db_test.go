package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/vars"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// wrong passwords lock out the IP they come from, not the owner of the account; spread over many IPs they
// lock out the new browsers, never the ones the owner logged in from
func TestLoginLockout(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	savedDomain := vars.Domain
	vars.Domain = "https://sport.example.com"
	defer func() { vars.Domain = savedDomain }()
	createUsers(t, db, "password", "dave")
	client := sessionClient{t, sessionRouter(&app.App{DB: *db, Logger: zerolog.Nop()})}

	login := func(ip, password string, known *http.Cookie) (int, []*http.Cookie) {
		t.Helper()
		code, _, cookies := client.request(http.MethodPost, "/login",
			`{"username": "dave", "password": "`+password+`"}`, func(req *http.Request) {
				req.Header.Set("Sec-Fetch-Site", "same-origin")
				req.Header.Set("Origin", vars.Domain)
				req.Header.Set("X-Forwarded-For", ip)
				if known != nil {
					req.AddCookie(&http.Cookie{Name: known.Name, Value: known.Value})
				}
			})
		return code, cookies
	}

	// dave logs in on his laptop: it becomes a known browser
	code, cookies := login("198.51.100.1", "password", nil)
	var laptop *http.Cookie
	for _, cookie := range cookies {
		if strings.HasSuffix(cookie.Name, "msw_known") {
			laptop = cookie
		}
	}
	if code != http.StatusOK || laptop == nil || laptop.Name != "__Host-msw_known" {
		t.Fatalf("first login: %d, known-device cookie %+v", code, laptop)
	}

	// an attacker guessing from one IP locks out that IP only, the right password included
	for i := 0; i < maxLoginFailures; i++ {
		if code, _ := login("203.0.113.1", "guess", nil); code != http.StatusBadRequest {
			t.Fatalf("guess %d: %d", i+1, code)
		}
	}
	if code, _ := login("203.0.113.1", "password", nil); code != http.StatusTooManyRequests {
		t.Errorf("the attacker's IP after %d failures: %d", maxLoginFailures, code)
	}
	// dave, from a new browser elsewhere, isn't locked out
	if code, _ := login("198.51.100.2", "password", nil); code != http.StatusOK {
		t.Errorf("dave from a new browser during the attack: %d", code)
	}

	// guesses spread over many IPs, maxLoginFailures each: the username is locked for new browsers
	for i := 0; i < maxAccountFailures; i++ {
		ip := fmt.Sprintf("203.0.113.%d", 10+i/maxLoginFailures)
		if code, _ := login(ip, "guess", nil); code != http.StatusBadRequest {
			t.Fatalf("distributed guess %d from %s: %d", i+1, ip, code)
		}
	}
	if code, _ := login("203.0.113.99", "password", nil); code != http.StatusTooManyRequests {
		t.Errorf("a new IP and browser after %d failures on the account: %d", maxAccountFailures, code)
	}
	if code, _ := login("198.51.100.3", "password", nil); code != http.StatusTooManyRequests {
		t.Errorf("dave from a new browser during the distributed attack: %d", code)
	}
	// a known-device cookie of another username doesn't help
	createUsers(t, db, "password", "erin")
	code, _, cookies = client.request(http.MethodPost, "/login", `{"username": "erin", "password": "password"}`,
		func(req *http.Request) {
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			req.Header.Set("Origin", vars.Domain)
			req.Header.Set("X-Forwarded-For", "198.51.100.5")
		})
	var erins *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == "__Host-msw_known" {
			erins = cookie
		}
	}
	if code != http.StatusOK || erins == nil {
		t.Fatalf("erin's login: %d", code)
	}
	if code, _ := login("198.51.100.6", "password", erins); code != http.StatusTooManyRequests {
		t.Errorf("dave with erin's known-device cookie: %d", code)
	}
	// dave's laptop still logs in, from any IP; the success ends the lock
	if code, _ := login("198.51.100.4", "password", laptop); code != http.StatusOK {
		t.Errorf("dave's known browser during the distributed attack: %d", code)
	}
	if code, _ := login("198.51.100.7", "password", nil); code != http.StatusOK {
		t.Errorf("a new browser after dave logged in: %d", code)
	}
}
