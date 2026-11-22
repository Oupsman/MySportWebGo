package apicontrollers

import (
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// nowPlus is the time seconds from now
func nowPlus(seconds int) time.Time { return time.Now().Add(time.Duration(seconds) * time.Second) }

func twoFactorRouter(t *testing.T, db *models.DB) sessionClient {
	client := registerRouter(t, db)
	r := client.router
	r.GET("/session", middlewares.IsAuthorized(), GetSession)
	r.GET("/2fa", middlewares.IsAuthorized(), TwoFactorStatus)
	r.POST("/2fa/setup", middlewares.IsAuthorized(), SetupTwoFactor)
	r.POST("/2fa/enable", middlewares.IsAuthorized(), EnableTwoFactor)
	r.POST("/2fa/disable", middlewares.IsAuthorized(), DisableTwoFactor)
	r.POST("/2fa/recovery", middlewares.IsAuthorized(), NewRecoveryCodes)
	return client
}

// jsonAnswer decodes an answer of rawRequest
func jsonAnswer(t *testing.T, body []byte) map[string]interface{} {
	t.Helper()
	answer := map[string]interface{}{}
	json.Unmarshal(body, &answer)
	return answer
}

func TestTwoFactor(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	client := twoFactorRouter(t, db)
	createUsers(t, db, "password", "alice")
	cookie := client.loginCookie("alice", "password")

	if code, _ := rawRequest(client, http.MethodPost, "/2fa/setup", cookie, `{"password": "wrong"}`); code != http.StatusUnauthorized {
		t.Errorf("setup with a wrong password: %d", code)
	}
	code, body := rawRequest(client, http.MethodPost, "/2fa/setup", cookie, `{"password": "password"}`)
	setup := jsonAnswer(t, body)
	secret, _ := setup["secret"].(string)
	if code != http.StatusOK || secret == "" || !strings.HasPrefix(setup["qr_code"].(string), "data:image/png;base64,") ||
		!strings.Contains(setup["url"].(string), "issuer=MySportWeb") {
		t.Fatalf("setup: %d %v", code, setup)
	}
	// stored encrypted
	var stored string
	db.Model(&models.Users{}).Where("username = ?", "alice").Pluck("totp_secret", &stored)
	if stored == "" || stored == secret {
		t.Errorf("stored secret %q", stored)
	}
	if code, _ := rawRequest(client, http.MethodPost, "/2fa/enable", cookie, `{"code": "000000"}`); code != http.StatusBadRequest {
		t.Errorf("enable with a wrong code: %d", code)
	}
	// still off until confirmed: the login needs no code
	client.loginCookie("alice", "password")

	code, body = rawRequest(client, http.MethodPost, "/2fa/enable", cookie,
		`{"code": "`+nextCode(t, db, "alice", secret)+`"}`)
	enabled := jsonAnswer(t, body)
	recovery, _ := enabled["recovery_codes"].([]interface{})
	if code != http.StatusOK || len(recovery) != utils.RecoveryCodes {
		t.Fatalf("enable: %d %v", code, enabled)
	}
	// the session of the request was replaced by a verified one
	if code, _ := rawRequest(client, http.MethodGet, "/session", cookie, ""); code != http.StatusUnauthorized {
		t.Errorf("the session before the setup: %d", code)
	}

	// the login: the password gives a challenge, no session
	code, answer, cookies := client.browser(http.MethodPost, "/login", nil, `{"username": "alice", "password": "password"}`)
	challenge := answer["challenge"]
	if code != http.StatusOK || challenge == "" || len(cookies) != 0 {
		t.Fatalf("login: %d %v %v", code, answer, cookies)
	}
	// the challenge isn't a session
	if code, _ := client.send(http.MethodGet, "/session", challenge, ""); code != http.StatusUnauthorized {
		t.Errorf("challenge as a session: %d", code)
	}
	second := func(code string) (int, map[string]string, []*http.Cookie) {
		return client.browser(http.MethodPost, "/login/2fa", nil, `{"challenge": "`+challenge+`", "code": "`+code+`"}`)
	}
	if code, _, _ := second("123456"); code != http.StatusBadRequest {
		t.Errorf("wrong code: %d", code)
	}
	good := nextCode(t, db, "alice", secret)
	code, answer, cookies = second(good)
	if code != http.StatusOK {
		t.Fatalf("second step: %d %v", code, answer)
	}
	verified := client.sessionCookie(cookies)
	code, body = rawRequest(client, http.MethodGet, "/session", verified, "")
	if session, _ := jsonAnswer(t, body)["session"].(map[string]interface{}); code != http.StatusOK ||
		session["mfa"] != true || session["two_factor"] != true {
		t.Errorf("session: %d %s", code, body)
	}
	// the same code again: refused (replay)
	if code, _, _ := second(good); code != http.StatusBadRequest {
		t.Errorf("replayed code: %d", code)
	}
	// a recovery code, once
	rc := recovery[0].(string)
	if code, _, _ := second(strings.ToUpper(rc)); code != http.StatusOK {
		t.Errorf("recovery code: %d", code)
	}
	if code, _, _ := second(rc); code != http.StatusBadRequest {
		t.Errorf("recovery code used twice: %d", code)
	}
	code, body = rawRequest(client, http.MethodGet, "/2fa", verified, "")
	if status := jsonAnswer(t, body); code != http.StatusOK || status["enabled"] != true ||
		status["recovery_codes_left"] != float64(utils.RecoveryCodes-1) {
		t.Errorf("status: %d %v", code, status)
	}

	// a minute later: the codes of now are new again
	passTime := func() {
		db.Model(&models.Users{}).Where("username = ?", "alice").Update("totp_last_step", 0)
	}
	passTime()
	// new recovery codes: the password and a code
	code, body = rawRequest(client, http.MethodPost, "/2fa/recovery", verified,
		`{"password": "password", "code": "`+nextCode(t, db, "alice", secret)+`"}`)
	if codes, _ := jsonAnswer(t, body)["recovery_codes"].([]interface{}); code != http.StatusOK || len(codes) != utils.RecoveryCodes {
		t.Errorf("new recovery codes: %d %s", code, body)
	}

	// an admin can't turn it off; a user can, with the password and a code
	db.Model(&models.Users{}).Where("username = ?", "alice").Update("role", models.RoleAdmin)
	if code, _ := rawRequest(client, http.MethodPost, "/2fa/disable", verified, `{"password": "password"}`); code != http.StatusForbidden {
		t.Errorf("admin disabling: %d", code)
	}
	db.Model(&models.Users{}).Where("username = ?", "alice").Update("role", models.RoleUser)
	if code, _ := rawRequest(client, http.MethodPost, "/2fa/disable", verified, `{"password": "password", "code": "1"}`); code != http.StatusBadRequest {
		t.Errorf("disabling with a wrong code: %d", code)
	}
	passTime()
	if code, body := rawRequest(client, http.MethodPost, "/2fa/disable", verified,
		`{"password": "password", "code": "`+nextCode(t, db, "alice", secret)+`"}`); code != http.StatusOK {
		t.Errorf("disable: %d %s", code, body)
	}
	client.loginCookie("alice", "password") // no code anymore

	// the command line turns it off for a user who lost everything
	secret = enableTOTP(t, db, "alice")
	if found, err := db.ResetTOTP("alice"); err != nil || !found {
		t.Fatalf("reset: %v %v", found, err)
	}
	client.loginCookie("alice", "password")
}

func TestDisabledAccount(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	client := twoFactorRouter(t, db)
	users := createUsers(t, db, "password", "alice")
	cookie := client.loginCookie("alice", "password")
	token, hash := utils.NewDeviceToken()
	if err := db.CreateDeviceToken(&models.DeviceTokens{UserID: users[0].ID, Name: "sync", Hash: hash,
		Permissions: models.ActivitiesRead, ExpiresAt: nowPlus(3600)}); err != nil {
		t.Fatal(err)
	}
	client.router.GET("/read", middlewares.IsAuthorized(models.ActivitiesRead), func(c *gin.Context) { c.Status(200) })
	if code, _ := client.send(http.MethodGet, "/read", token, ""); code != http.StatusOK {
		t.Fatalf("device token before: %d", code)
	}

	if found, err := db.SetUserDisabled(users[0].ID, true); err != nil || !found {
		t.Fatalf("disable: %v %v", found, err)
	}
	if code, _ := rawRequest(client, http.MethodGet, "/session", cookie, ""); code != http.StatusUnauthorized {
		t.Errorf("session of a disabled user: %d", code)
	}
	if code, _ := client.send(http.MethodGet, "/read", token, ""); code != http.StatusUnauthorized {
		t.Errorf("device token of a disabled user: %d", code)
	}
	code, answer, _ := client.browser(http.MethodPost, "/login", nil, `{"username": "alice", "password": "password"}`)
	if code != http.StatusForbidden || !strings.Contains(answer["error"], "disabled") {
		t.Errorf("login of a disabled user: %d %v", code, answer)
	}
	// a wrong password says nothing of the account
	code, answer, _ = client.browser(http.MethodPost, "/login", nil, `{"username": "alice", "password": "nope"}`)
	if strings.Contains(answer["error"], "disabled") {
		t.Errorf("wrong password of a disabled user: %d %v", code, answer)
	}

	if _, err := db.SetUserDisabled(users[0].ID, false); err != nil {
		t.Fatal(err)
	}
	if code, _ := rawRequest(client, http.MethodGet, "/session", cookie, ""); code != http.StatusOK {
		t.Errorf("session once enabled again: %d", code)
	}
}
