package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/vars"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
)

func testDB(t *testing.T) *models.DB {
	dsn := os.Getenv("MYSPORTWEB_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSPORTWEB_TEST_DSN is not set")
	}
	db, err := models.ConnectToDB("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	var name string
	if err := db.Raw("SELECT current_database()").Scan(&name).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(name, "test") {
		t.Fatalf("refusing to empty database %q, its name must contain \"test\"", name)
	}
	if err := models.CreateOrMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("TRUNCATE revoked_sessions, users RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	return db
}

type sessionClient struct {
	t      *testing.T
	router *gin.Engine
}

func (s sessionClient) send(method, path, token, body string) (int, map[string]string) {
	code, answer, _ := s.request(method, path, body, func(req *http.Request) {
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	})
	return code, answer
}

// request sends a request prepared by prepare, returning the status, the string fields of the answer and
// the cookies it sets
func (s sessionClient) request(method, path, body string, prepare func(*http.Request)) (int, map[string]string, []*http.Cookie) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	prepare(req)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	answer := map[string]string{}
	var raw map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &raw)
	for k, v := range raw {
		switch value := v.(type) {
		case string:
			answer[k] = value
		case map[string]interface{}:
			// the session: its fields as text
			for field, f := range value {
				answer[k+"."+field] = fmt.Sprint(f)
			}
		}
	}
	return w.Code, answer, w.Result().Cookies()
}

// browser sends a request as the web application: with the session cookie (when not nil), from the
// origin of the app
func (s sessionClient) browser(method, path string, cookie *http.Cookie, body string) (int, map[string]string, []*http.Cookie) {
	return s.request(method, path, body, func(req *http.Request) {
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Origin", vars.Domain)
		if cookie != nil {
			req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
		}
	})
}

func (s sessionClient) login(username, password string) (int, map[string]string) {
	code, answer, _ := s.browser(http.MethodPost, "/login", nil, `{"username": "`+username+`", "password": "`+password+`"}`)
	return code, answer
}

// loginCookie logs in, returning the session cookie set
func (s sessionClient) loginCookie(username, password string) *http.Cookie {
	s.t.Helper()
	code, answer, cookies := s.browser(http.MethodPost, "/login", nil, `{"username": "`+username+`", "password": "`+password+`"}`)
	if code != http.StatusOK {
		s.t.Fatalf("login: %d %v", code, answer)
	}
	return s.sessionCookie(cookies)
}

func (s sessionClient) sessionCookie(cookies []*http.Cookie) *http.Cookie {
	s.t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == middlewares.SessionCookieName() {
			return cookie
		}
	}
	s.t.Fatalf("no cookie %s in %v", middlewares.SessionCookieName(), cookies)
	return nil
}

// sessionRouter serves the session routes as the webserver does, with an anonymous-or-user route
func sessionRouter(App *app.App) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("App", App); c.Next() })
	router.Use(middlewares.CrossOriginProtection())
	router.POST("/login", Login)
	router.POST("/logout", Logout)
	router.GET("/session", middlewares.IsAuthorized(), GetSession)
	router.GET("/user", middlewares.IsAuthorized(), GetUser)
	router.POST("/user", middlewares.IsAuthorized(), UpdateUser)
	router.GET("/optional", middlewares.OptionalUser(), func(c *gin.Context) {
		_, known := middlewares.LookupUser(c)
		c.JSON(http.StatusOK, gin.H{"known": fmt.Sprint(known)})
	})
	return router
}

func createUsers(t *testing.T, db *models.DB, password string, names ...string) []models.Users {
	// a cheap hash: the tests log in many times
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	var users []models.Users
	for _, name := range names {
		user := models.Users{Username: name, Email: name + "@test.local", Password: string(hash), Role: "User"}
		if err := db.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
		users = append(users, user)
	}
	return users
}

func TestSessions(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	savedDomain := vars.Domain
	vars.Domain = "https://sport.example.com"
	defer func() { vars.Domain = savedDomain }()
	alice := createUsers(t, db, "old password", "alice", "bob")[0]
	App := &app.App{DB: *db, Logger: zerolog.Nop()}
	client := sessionClient{t, sessionRouter(App)}

	code, answer, cookies := client.browser(http.MethodPost, "/login", nil, `{"username": "alice", "password": "old password"}`)
	if code != http.StatusOK || answer["token"] != "" || answer["session.sub"] != fmt.Sprint(alice.ID) ||
		answer["session.uuid"] != alice.UUID.String() {
		t.Fatalf("login: %d %v (the session, never the token)", code, answer)
	}
	phone := client.sessionCookie(cookies)
	if phone.Name != "__Host-msw_session" || !phone.HttpOnly || !phone.Secure || phone.SameSite != http.SameSiteStrictMode ||
		phone.Path != "/" || phone.Domain != "" || phone.MaxAge < 29*24*3600 {
		t.Errorf("cookie over HTTPS: %+v", phone)
	}
	laptop := client.loginCookie("alice", "old password")

	if code, answer, _ := client.browser(http.MethodGet, "/session", phone, ""); code != http.StatusOK ||
		answer["session.uuid"] != alice.UUID.String() || answer["session.exp"] == "" {
		t.Errorf("session: %d %v", code, answer)
	}
	if code, _, _ := client.browser(http.MethodGet, "/session", nil, ""); code != http.StatusUnauthorized {
		t.Errorf("session without cookie: %d", code)
	}
	// the session token also works in the Authorization header (API clients)
	if code, _ := client.send(http.MethodGet, "/user", laptop.Value, ""); code != http.StatusOK {
		t.Errorf("session token in the header: %d", code)
	}

	// a wrong password and an unknown user get the same answer
	_, wrongPassword := client.login("bob", "nope")
	_, unknownUser := client.login("nobody", "nope")
	if wrongPassword["error"] != unknownUser["error"] || wrongPassword["error"] == "" {
		t.Errorf("wrong password %q, unknown user %q", wrongPassword["error"], unknownUser["error"])
	}

	// logging out ends that session only, and clears its cookie
	code, answer, cookies = client.browser(http.MethodPost, "/logout", phone, "")
	if cleared := client.sessionCookie(cookies); code != http.StatusOK || cleared.MaxAge >= 0 || cleared.Value != "" {
		t.Fatalf("logout: %d %v, cookie %+v", code, answer, cleared)
	}
	if code, _, _ := client.browser(http.MethodGet, "/user", phone, ""); code != http.StatusUnauthorized {
		t.Errorf("logged out cookie: %d", code)
	}
	if code, _, _ := client.browser(http.MethodGet, "/user", laptop, ""); code != http.StatusOK {
		t.Errorf("other session after a logout: %d", code)
	}
	// the cookie of an ended session, or none: still cleared, the browser gets rid of it
	for name, cookie := range map[string]*http.Cookie{"ended session": phone, "no session": nil,
		"garbage": {Name: middlewares.SessionCookieName(), Value: "garbage"}} {
		code, _, cookies := client.browser(http.MethodPost, "/logout", cookie, "")
		if code != http.StatusOK || client.sessionCookie(cookies).MaxAge >= 0 {
			t.Errorf("logout with %s: %d", name, code)
		}
	}
	// public pages: the cookie of an ended session is anonymous, an invalid header is refused
	if code, answer, _ := client.browser(http.MethodGet, "/optional", phone, ""); code != http.StatusOK || answer["known"] != "false" {
		t.Errorf("optional user, ended session: %d %v", code, answer)
	}
	if code, answer, _ := client.browser(http.MethodGet, "/optional", laptop, ""); code != http.StatusOK || answer["known"] != "true" {
		t.Errorf("optional user, session: %d %v", code, answer)
	}
	if code, _ := client.send(http.MethodGet, "/optional", phone.Value, ""); code != http.StatusUnauthorized {
		t.Errorf("optional user, ended session in the header: %d", code)
	}

	// changing the password ends the sessions, the answer sets a new cookie
	code, answer, cookies = client.browser(http.MethodPost, "/user", laptop, `{"oldpassword": "old password", `+
		`"newPassword": "new password", "newPasswordConfirmation": "new password", "email": "alice@test.local"}`)
	if code != http.StatusOK || answer["token"] != "" || answer["session.sub"] != fmt.Sprint(alice.ID) {
		t.Fatalf("password change: %d %v", code, answer)
	}
	renewed := client.sessionCookie(cookies)
	if code, _, _ := client.browser(http.MethodGet, "/user", laptop, ""); code != http.StatusUnauthorized {
		t.Errorf("session opened with the old password: %d", code)
	}
	if code, _, _ := client.browser(http.MethodGet, "/user", renewed, ""); code != http.StatusOK {
		t.Errorf("cookie set with the new password: %d", code)
	}
	if code, _ := client.login("alice", "new password"); code != http.StatusOK {
		t.Errorf("login with the new password: %d", code)
	}
	// the profile change keeps the sessions
	code, _, _ = client.browser(http.MethodPost, "/user", renewed, `{"oldpassword": "new password", "email": "alice@test.local", "height": 180}`)
	if code != http.StatusOK {
		t.Errorf("profile change: %d", code)
	}
	if code, _, _ := client.browser(http.MethodGet, "/user", renewed, ""); code != http.StatusOK {
		t.Errorf("session after a profile change: %d", code)
	}

	// bob had one failure above: 4 more lock his username, even with the right password, other users go on
	for i := 0; i < maxLoginFailures-1; i++ {
		if code, _ := client.login("Bob", "nope"); code != http.StatusBadRequest {
			t.Fatalf("failure %d: %d", i+2, code)
		}
	}
	if code, _ := client.login("bob", "old password"); code != http.StatusTooManyRequests {
		t.Errorf("locked username: %d", code)
	}
	if code, _ := client.login("alice", "new password"); code != http.StatusOK {
		t.Errorf("another user while bob is locked: %d", code)
	}
}

// the requests a page of another origin makes the browser send with the cookie are refused (CSRF), the
// ones of the app and of the clients that aren't browsers go through
func TestCrossOriginRequests(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	savedDomain := vars.Domain
	vars.Domain = "https://sport.example.com"
	defer func() { vars.Domain = savedDomain }()
	createUsers(t, db, "password", "carol")
	client := sessionClient{t, sessionRouter(&app.App{DB: *db, Logger: zerolog.Nop()})}
	cookie := client.loginCookie("carol", "password")
	update := `{"oldpassword": "password", "email": "carol@test.local", "height": 170}`

	for _, c := range []struct {
		about   string
		headers map[string]string
		want    int
	}{
		{"another site", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}, http.StatusForbidden},
		{"a subdomain (same site)", map[string]string{"Sec-Fetch-Site": "same-site", "Origin": "https://blog.example.com"}, http.StatusForbidden},
		{"an old browser, another origin", map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden},
		{"the app", map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": vars.Domain}, http.StatusOK},
		{"an old browser, the app", map[string]string{"Origin": vars.Domain}, http.StatusOK},
		{"not a browser", map[string]string{}, http.StatusOK},
	} {
		code, answer, _ := client.request(http.MethodPost, "/user", update, func(req *http.Request) {
			for k, v := range c.headers {
				req.Header.Set(k, v)
			}
			req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
		})
		if code != c.want {
			t.Errorf("%s: %d %v, want %d", c.about, code, answer, c.want)
		}
	}
	// reading stays open: the answer can't be read by the other origin (CORS)
	code, _, _ := client.request(http.MethodGet, "/session", "", func(req *http.Request) {
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	})
	if code != http.StatusOK {
		t.Errorf("cross-site GET: %d", code)
	}
	// nor a login from another site (a session of the attacker in the victim's browser)
	code, _, _ = client.request(http.MethodPost, "/login", `{"username": "carol", "password": "password"}`, func(req *http.Request) {
		req.Header.Set("Sec-Fetch-Site", "cross-site")
	})
	if code != http.StatusForbidden {
		t.Errorf("cross-site login: %d", code)
	}
}

// over HTTP (development, a local network) the cookie can't be Secure, nor have the __Host- prefix
func TestSessionCookieOverHTTP(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	savedDomain := vars.Domain
	vars.Domain = "http://localhost:3000"
	defer func() { vars.Domain = savedDomain }()
	createUsers(t, db, "password", "dave")
	client := sessionClient{t, sessionRouter(&app.App{DB: *db, Logger: zerolog.Nop()})}
	cookie := client.loginCookie("dave", "password")
	if cookie.Name != "msw_session" || cookie.Secure || !cookie.HttpOnly {
		t.Errorf("cookie over HTTP: %+v", cookie)
	}
	if code, _, _ := client.browser(http.MethodGet, "/user", cookie, ""); code != http.StatusOK {
		t.Errorf("with the cookie: %d", code)
	}
}
