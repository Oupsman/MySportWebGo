package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
)

const testSetupCode = "setupcodeoftheinstance0000"

// registerRouter serves the registration, the instance and the admin routes as the webserver does, for an
// instance started without admin (its setup code is testSetupCode)
func registerRouter(t *testing.T, db *models.DB) sessionClient {
	// the registrations hash passwords: cheaply in the tests
	savedCost := utils.PasswordCost
	utils.PasswordCost = bcrypt.MinCost
	t.Cleanup(func() { utils.PasswordCost = savedCost })
	gin.SetMode(gin.TestMode)
	router := gin.New()
	App := &app.App{DB: *db, Logger: zerolog.Nop(), SetupCode: testSetupCode}
	router.Use(func(c *gin.Context) { c.Set("App", App); c.Next() })
	router.POST("/register", Register)
	router.POST("/login", Login)
	router.POST("/login/2fa", LoginSecondFactor)
	router.GET("/instance", Instance)
	router.GET("/admin/invitations", middlewares.IsAdmin(), GetInvitations)
	router.POST("/admin/invitations", middlewares.IsAdmin(), CreateInvitation)
	router.DELETE("/admin/invitations/:id", middlewares.IsAdmin(), RevokeInvitation)
	return sessionClient{t: t, router: router}
}

// registration sets REGISTRATION for a test
func registration(t *testing.T, mode vars.RegistrationMode) {
	saved := vars.Registration
	vars.Registration = mode
	t.Cleanup(func() { vars.Registration = saved })
}

func registerWith(client sessionClient, username, email, password, extra string) (int, map[string]string) {
	return client.send(http.MethodPost, "/register", "",
		fmt.Sprintf(`{"username": %q, "email": %q, "password": %q%s}`, username, email, password, extra))
}

func register(client sessionClient, username, email, password string) (int, map[string]string) {
	return registerWith(client, username, email, password, "")
}

func registerAdmin(client sessionClient, username string) (int, map[string]string) {
	return registerWith(client, username, username+"@test.local", "a long password", `, "setup_code": "`+testSetupCode+`"`)
}

func registerInvited(client sessionClient, username, email, code string) (int, map[string]string) {
	return registerWith(client, username, email, "a long password", `, "invitation": "`+code+`"`)
}

func role(t *testing.T, db *models.DB, username string) string {
	var user models.Users
	if err := db.Unscoped().Where("username = ?", username).First(&user).Error; err != nil {
		t.Fatalf("%s: %v", username, err)
	}
	return user.Role
}

func countUsers(t *testing.T, db *models.DB) int64 {
	var count int64
	if err := db.Unscoped().Model(&models.Users{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func TestRegister(t *testing.T) {
	db := testDB(t)
	registration(t, vars.RegistrationOpen)
	client := registerRouter(t, db)

	// open registrations: users, the first one included, never the admin
	if code, answer := register(client, "first", "first@test.local", "a long password"); code != http.StatusCreated {
		t.Fatalf("first registration: %d %v", code, answer)
	}
	if code, answer := register(client, "second", "second@test.local", "a long password"); code != http.StatusCreated {
		t.Fatalf("second registration: %d %v", code, answer)
	}
	if role(t, db, "first") != models.RoleUser || role(t, db, "second") != models.RoleUser {
		t.Errorf("roles: first %s, second %s", role(t, db, "first"), role(t, db, "second"))
	}

	// a deleted user keeps its username and email
	if err := db.Where("username = ?", "second").Delete(&models.Users{}).Error; err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, username, email, password string
		want                            int
	}{
		{"taken username", "first", "other@test.local", "a long password", http.StatusConflict},
		// was answered 201 without creating anything
		{"taken email", "other", "first@test.local", "a long password", http.StatusConflict},
		{"taken email, other case", "other", "First@Test.local", "a long password", http.StatusConflict},
		{"username of a deleted user", "second", "other@test.local", "a long password", http.StatusConflict},
		{"email of a deleted user", "other", "second@test.local", "a long password", http.StatusConflict},
		{"invalid email", "other", "not an email", "a long password", http.StatusBadRequest},
		{"email with a name", "other", "Other <other@test.local>", "a long password", http.StatusBadRequest},
		{"missing password", "other", "other@test.local", "", http.StatusBadRequest},
		{"blank username", "  ", "other@test.local", "a long password", http.StatusBadRequest},
		{"password too long for bcrypt", "other", "other@test.local", strings.Repeat("x", 73), http.StatusBadRequest},
		{"password too short", "other", "other@test.local", "password", http.StatusBadRequest},
		{"common password", "other", "other@test.local", "qwertyuiop", http.StatusBadRequest},
		{"password of the username", "other", "other@test.local", "other12345", http.StatusBadRequest},
	} {
		if code, answer := register(client, tt.username, tt.email, tt.password); code != tt.want {
			t.Errorf("%s: %d %v, want %d", tt.name, code, answer, tt.want)
		}
	}
	if count := countUsers(t, db); count != 2 {
		t.Errorf("%d users, want the first two only", count)
	}
}

func TestRegisterModes(t *testing.T) {
	db := testDB(t)
	client := registerRouter(t, db)

	instance := func() (string, string) {
		code, body := rawRequest(client, http.MethodGet, "/instance", nil, "")
		var answer map[string]interface{}
		if err := json.Unmarshal(body, &answer); err != nil || code != http.StatusOK {
			t.Fatalf("instance: %d %s", code, body)
		}
		return fmt.Sprint(answer["registration"]), fmt.Sprint(answer["setup_required"])
	}

	registration(t, vars.RegistrationClosed)
	if mode, setup := instance(); mode != "closed" || setup != "true" {
		t.Errorf("new instance: registration %s, setup required %s", mode, setup)
	}
	if code, answer := register(client, "visitor", "visitor@test.local", "a long password"); code != http.StatusForbidden {
		t.Errorf("closed: %d %v", code, answer)
	}
	if code, answer := registerWith(client, "visitor", "visitor@test.local", "a long password", `, "setup_code": "wrong"`); code != http.StatusForbidden {
		t.Errorf("wrong setup code: %d %v", code, answer)
	}
	if count := countUsers(t, db); count != 0 {
		t.Fatalf("%d users after the refused registrations", count)
	}

	// the setup code registers the admin, whatever the mode; its case and spaces don't matter
	code, answer := registerWith(client, "boss", "boss@test.local", "a long password",
		`, "setup_code": " `+strings.ToUpper(testSetupCode)+` "`)
	if code != http.StatusCreated || answer["role"] != models.RoleAdmin || role(t, db, "boss") != models.RoleAdmin {
		t.Fatalf("setup: %d %v", code, answer)
	}
	if _, setup := instance(); setup != "false" {
		t.Errorf("setup still required once the admin exists")
	}
	// once only
	registration(t, vars.RegistrationOpen)
	if code, answer := registerAdmin(client, "usurper"); code != http.StatusForbidden {
		t.Errorf("second setup: %d %v", code, answer)
	}
	if role(t, db, "boss") != models.RoleAdmin || countUsers(t, db) != 1 {
		t.Errorf("the second setup changed the users")
	}

	registration(t, vars.RegistrationInvite)
	if mode, _ := instance(); mode != "invite" {
		t.Errorf("registration %s, want invite", mode)
	}
	if code, answer := register(client, "visitor", "visitor@test.local", "a long password"); code != http.StatusForbidden {
		t.Errorf("invite without invitation: %d %v", code, answer)
	}
	if code, answer := registerInvited(client, "visitor", "visitor@test.local", utils.NewCode()); code != http.StatusForbidden {
		t.Errorf("unknown invitation: %d %v", code, answer)
	}
	if count := countUsers(t, db); count != 1 {
		t.Errorf("%d users, want the admin only", count)
	}
}

// simultaneous setups: one admin only
func TestRegisterOneAdmin(t *testing.T) {
	db := testDB(t)
	registration(t, vars.RegistrationClosed)
	client := registerRouter(t, db)

	const n = 4
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _ = registerAdmin(client, fmt.Sprintf("user%d", i))
		}()
	}
	wg.Wait()
	created := 0
	for i, code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusForbidden:
		default:
			t.Errorf("user%d: %d", i, code)
		}
	}
	var admins int64
	if err := db.Model(&models.Users{}).Where("role = ?", models.RoleAdmin).Count(&admins).Error; err != nil {
		t.Fatal(err)
	}
	if created != 1 || admins != 1 || countUsers(t, db) != 1 {
		t.Errorf("%d created, %d admins, %d users: want 1", created, admins, countUsers(t, db))
	}
}

// rawRequest sends a request with the session cookie, returning the status and the body
func rawRequest(client sessionClient, method, path string, cookie *http.Cookie, body string) (int, []byte) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	}
	w := httptest.NewRecorder()
	client.router.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

type createdInvitation struct {
	Invitation models.Invitations `json:"invitation"`
	Code       string             `json:"code"`
	Link       string             `json:"link"`
}

func invite(t *testing.T, client sessionClient, admin *http.Cookie, body string) createdInvitation {
	t.Helper()
	code, answer := rawRequest(client, http.MethodPost, "/admin/invitations", admin, body)
	if code != http.StatusCreated {
		t.Fatalf("invitation %s: %d %s", body, code, answer)
	}
	var created createdInvitation
	if err := json.Unmarshal(answer, &created); err != nil {
		t.Fatal(err)
	}
	return created
}

func TestInvitations(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	savedDomain := vars.Domain
	vars.Domain = "https://sport.example.com"
	defer func() { vars.Domain = savedDomain }()
	registration(t, vars.RegistrationInvite)
	client := registerRouter(t, db)

	if code, answer := registerAdmin(client, "boss"); code != http.StatusCreated {
		t.Fatalf("setup: %d %v", code, answer)
	}
	// an admin without two-factor authentication: no admin page
	plain := client.loginCookie("boss", "a long password")
	if code, body := rawRequest(client, http.MethodGet, "/admin/invitations", plain, ""); code != http.StatusForbidden ||
		!strings.Contains(string(body), "mfa_required") {
		t.Errorf("admin without 2FA: %d %s", code, body)
	}
	secret := enableTOTP(t, db, "boss")
	admin := client.loginCookieMFA(db, "boss", "a long password", secret)

	// the admin routes: the admins' sessions only
	user := createUsers(t, db, "a long password", "alice")[0]
	alice := client.loginCookie("alice", "a long password")
	token, hash := utils.NewDeviceToken()
	bossID := uint(0)
	db.Model(&models.Users{}).Where("username = ?", "boss").Pluck("id", &bossID)
	if err := db.CreateDeviceToken(&models.DeviceTokens{UserID: bossID, Name: "script", Hash: hash, Hint: token[:8],
		Permissions: models.ActivitiesRead, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	for name, try := range map[string]func() int{
		"anonymous": func() int { code, _ := rawRequest(client, http.MethodGet, "/admin/invitations", nil, ""); return code },
		"user": func() int {
			code, _ := rawRequest(client, http.MethodGet, "/admin/invitations", alice, "")
			return code
		},
		"user creating": func() int {
			code, _ := rawRequest(client, http.MethodPost, "/admin/invitations", alice, "{}")
			return code
		},
		"device token of an admin": func() int {
			code, _ := client.send(http.MethodGet, "/admin/invitations", token, "")
			return code
		},
	} {
		if code := try(); code != http.StatusUnauthorized && code != http.StatusForbidden {
			t.Errorf("%s: %d", name, code)
		}
	}
	// promoted: the admin pages once logged in with a second factor; demoted: refused at once, whatever its
	// session says
	db.Model(&models.Users{}).Where("id = ?", user.ID).Update("role", models.RoleAdmin)
	if code, _ := rawRequest(client, http.MethodGet, "/admin/invitations", alice, ""); code != http.StatusForbidden {
		t.Errorf("promoted user without 2FA: %d", code)
	}
	alice = client.loginCookieMFA(db, "alice", "a long password", enableTOTP(t, db, "alice"))
	if code, _ := rawRequest(client, http.MethodGet, "/admin/invitations", alice, ""); code != http.StatusOK {
		t.Errorf("promoted user: %d", code)
	}
	db.Model(&models.Users{}).Where("id = ?", user.ID).Update("role", models.RoleUser)
	if code, _ := rawRequest(client, http.MethodGet, "/admin/invitations", alice, ""); code != http.StatusForbidden {
		t.Errorf("demoted user: %d", code)
	}

	for _, body := range []string{`{"days": 91}`, `{"days": -1}`, `{"email": "not an email"}`} {
		if code, answer := rawRequest(client, http.MethodPost, "/admin/invitations", admin, body); code != http.StatusBadRequest {
			t.Errorf("invitation %s: %d %s", body, code, answer)
		}
	}

	// an invitation registers once
	first := invite(t, client, admin, "{}")
	if first.Link != "https://sport.example.com/#/signup?invitation="+first.Code || len(first.Code) != 26 ||
		first.Invitation.Hint != first.Code[:4] {
		t.Errorf("invitation: %+v", first)
	}
	if days := time.Until(first.Invitation.ExpiresAt).Hours() / 24; days < 6.9 || days > 7 {
		t.Errorf("valid %.1f days, want 7", days)
	}
	if code, answer := registerInvited(client, "guest", "guest@test.local", first.Code); code != http.StatusCreated ||
		role(t, db, "guest") != models.RoleUser {
		t.Fatalf("invited: %d %v", code, answer)
	}
	if code, answer := registerInvited(client, "guest2", "guest2@test.local", first.Code); code != http.StatusForbidden {
		t.Errorf("used invitation: %d %v", code, answer)
	}
	// a refused registration doesn't use it up
	second := invite(t, client, admin, "{}")
	if code, _ := registerInvited(client, "guest", "other@test.local", second.Code); code != http.StatusConflict {
		t.Errorf("taken username: %d", code)
	}
	if code, answer := registerInvited(client, "guest2", "guest2@test.local", second.Code); code != http.StatusCreated {
		t.Errorf("invitation after a refused registration: %d %v", code, answer)
	}

	// for one email only
	bound := invite(t, client, admin, `{"email": "carol@test.local", "days": 1}`)
	if code, _ := registerInvited(client, "mallory", "mallory@test.local", bound.Code); code != http.StatusForbidden {
		t.Errorf("invitation of another email: %d", code)
	}
	if code, answer := registerInvited(client, "carol", "Carol@Test.local", bound.Code); code != http.StatusCreated {
		t.Errorf("invitation of the email: %d %v", code, answer)
	}

	// expired
	expired := invite(t, client, admin, "{}")
	db.Model(&models.Invitations{}).Where("id = ?", expired.Invitation.ID).Update("expires_at", time.Now().Add(-time.Minute))
	if code, _ := registerInvited(client, "late", "late@test.local", expired.Code); code != http.StatusForbidden {
		t.Errorf("expired invitation: %d", code)
	}

	// revoked; a used one stays
	revoked := invite(t, client, admin, "{}")
	if code, answer := rawRequest(client, http.MethodDelete, "/admin/invitations/"+revoked.Invitation.ID.String(), admin, ""); code != http.StatusOK {
		t.Errorf("revoke: %d %s", code, answer)
	}
	if code, _ := registerInvited(client, "revoked", "revoked@test.local", revoked.Code); code != http.StatusForbidden {
		t.Errorf("revoked invitation: %d", code)
	}
	if code, _ := rawRequest(client, http.MethodDelete, "/admin/invitations/"+first.Invitation.ID.String(), admin, ""); code != http.StatusNotFound {
		t.Errorf("revoke a used invitation: %d", code)
	}

	// simultaneous uses: one registration
	shared := invite(t, client, admin, "{}")
	const n = 4
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _ = registerInvited(client, fmt.Sprintf("racer%d", i), fmt.Sprintf("racer%d@test.local", i), shared.Code)
		}()
	}
	wg.Wait()
	created := 0
	for _, code := range codes {
		if code == http.StatusCreated {
			created++
		}
	}
	if created != 1 {
		t.Errorf("%d registrations with one invitation: %v", created, codes)
	}

	// the list: who invited whom, never the codes nor their hashes
	code, body := rawRequest(client, http.MethodGet, "/admin/invitations", admin, "")
	if code != http.StatusOK || strings.Contains(string(body), first.Code) || strings.Contains(string(body), utils.HashCode(first.Code)) {
		t.Fatalf("list: %d %s", code, body)
	}
	var list struct {
		Invitations []struct {
			ID        string     `json:"id"`
			CreatedBy string     `json:"created_by"`
			UsedBy    string     `json:"used_by"`
			UsedAt    *time.Time `json:"used_at"`
			Email     string     `json:"email"`
		} `json:"invitations"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	// first, second, bound, expired, shared: the revoked one is gone
	if len(list.Invitations) != 5 {
		t.Fatalf("%d invitations listed, want 5: %s", len(list.Invitations), body)
	}
	for _, invitation := range list.Invitations {
		if invitation.CreatedBy != "boss" {
			t.Errorf("created by %q", invitation.CreatedBy)
		}
		if invitation.ID == first.Invitation.ID.String() && (invitation.UsedBy != "guest" || invitation.UsedAt == nil) {
			t.Errorf("first invitation: used by %q at %v", invitation.UsedBy, invitation.UsedAt)
		}
		if invitation.ID == bound.Invitation.ID.String() && invitation.Email != "carol@test.local" {
			t.Errorf("bound invitation: email %q", invitation.Email)
		}
	}
}
