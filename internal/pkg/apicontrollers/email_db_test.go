package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/mail"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/notifications"
	"MySportWeb/internal/pkg/vars"
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"MySportWeb/internal/pkg/utils"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
)

// recordingMailer keeps the messages instead of sending them; fail makes them fail
type recordingMailer struct {
	mu   sync.Mutex
	sent []mail.Message
	fail bool
}

func (m *recordingMailer) Send(_ context.Context, msg mail.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return errors.New("connecting to smtp.example.com:587: connection refused")
	}
	m.sent = append(m.sent, msg)
	return nil
}

func (m *recordingMailer) messages() []mail.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]mail.Message(nil), m.sent...)
}

var codeOfLink = regexp.MustCompile(`#/([a-z-]+)\?code=([a-z0-9%]+)`)

// linkCode returns the page and the code of the link of a message
func linkCode(t *testing.T, m mail.Message) (string, string) {
	t.Helper()
	found := codeOfLink.FindStringSubmatch(m.Text)
	if found == nil {
		t.Fatalf("no link in %q", m.Text)
	}
	code, _ := url.QueryUnescape(found[2])
	return found[1], code
}

func emailRouter(t *testing.T, db *models.DB, mailer mail.Mailer) (sessionClient, *app.App) {
	t.Helper()
	savedCost := utils.PasswordCost
	utils.PasswordCost = bcrypt.MinCost
	t.Cleanup(func() { utils.PasswordCost = savedCost })
	gin.SetMode(gin.TestMode)
	router := gin.New()
	App := &app.App{DB: *db, Logger: zerolog.Nop(), SetupCode: testSetupCode, Mailer: mailer,
		Notifications: notifications.New()}
	router.Use(func(c *gin.Context) { c.Set("App", App); c.Next() })
	router.POST("/register", Register)
	router.POST("/login", Login)
	router.POST("/login/2fa", LoginSecondFactor)
	router.GET("/instance", Instance)
	router.POST("/user", middlewares.IsAuthorized(), UpdateUser)
	router.GET("/email", middlewares.IsAuthorized(), EmailStatus)
	router.POST("/email/verify", VerifyEmail)
	router.POST("/email/resend", middlewares.IsAuthorized(), ResendVerification)
	router.POST("/forgot", ForgotPassword)
	router.POST("/password-reset", ResetPassword)
	router.GET("/notifications/email", middlewares.IsAuthorized(), EmailNotifications)
	router.POST("/notifications/email", middlewares.IsAuthorized(), SetEmailNotifications)
	router.POST("/admin/invitations", middlewares.IsAdmin(), CreateInvitation)
	router.POST("/admin/email/test", middlewares.IsAdmin(), SendTestEmail)
	return sessionClient{t: t, router: router}, App
}

func TestEmailVerification(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	vars.Domain = "https://sport.example.com"
	registration(t, vars.RegistrationOpen)
	mailer := &recordingMailer{}
	client, App := emailRouter(t, db, mailer)

	if code, answer, _ := client.browser(http.MethodGet, "/instance", nil, ""); code != http.StatusOK || answer["registration"] == "" {
		t.Fatalf("instance: %d %v", code, answer)
	}
	if code, answer := register(client, "zoe", "zoe@example.org", "password"); code != http.StatusCreated {
		t.Fatalf("register: %d %v", code, answer)
	}
	App.Wait(5 * time.Second)
	sent := mailer.messages()
	if len(sent) != 1 || sent[0].To.Address != "zoe@example.org" {
		t.Fatalf("sent %v", sent)
	}
	page, code := linkCode(t, sent[0])
	if page != "verify-email" {
		t.Errorf("link page %s", page)
	}
	cookie := client.loginCookie("zoe", "password")
	verify := func(code string) int {
		status, _, _ := client.browser(http.MethodPost, "/email/verify", nil, `{"code": "`+code+`"}`)
		return status
	}
	if status := verify("nope"); status != http.StatusBadRequest {
		t.Errorf("unknown code: %d", status)
	}
	if status := verify(code); status != http.StatusOK {
		t.Fatalf("verify: %d", status)
	}
	if status := verify(code); status != http.StatusBadRequest {
		t.Errorf("a link used twice: %d", status)
	}
	var user models.Users
	db.Where("username = ?", "zoe").First(&user)
	if user.EmailVerifiedAt == nil {
		t.Fatal("not confirmed")
	}

	// a new address: confirmed from its mailbox before it replaces the old one
	if status, answer := rawRequest(client, http.MethodPost, "/user", cookie,
		`{"oldpassword": "password", "email": "zoe@new.example"}`); status != http.StatusOK || !strings.Contains(string(answer), "zoe@new.example") {
		t.Fatalf("change: %d %s", status, answer)
	}
	App.Wait(5 * time.Second)
	db.Where("username = ?", "zoe").First(&user)
	if user.Email != "zoe@example.org" || user.EmailVerifiedAt == nil {
		t.Errorf("changed before its confirmation: %s", user.Email)
	}
	status, body := rawRequest(client, http.MethodGet, "/email", cookie, "")
	if status != http.StatusOK || !strings.Contains(string(body), `"pending":"zoe@new.example"`) {
		t.Errorf("status: %d %s", status, body)
	}
	sent = mailer.messages()
	if len(sent) != 2 || sent[1].To.Address != "zoe@new.example" {
		t.Fatalf("sent %v", sent)
	}
	_, code = linkCode(t, sent[1])
	if status := verify(code); status != http.StatusOK {
		t.Fatalf("verify the new one: %d", status)
	}
	db.Where("username = ?", "zoe").First(&user)
	if user.Email != "zoe@new.example" || user.EmailVerifiedAt == nil {
		t.Errorf("after confirming: %s %v", user.Email, user.EmailVerifiedAt)
	}
	// resend: nothing to confirm anymore
	if status, _ := rawRequest(client, http.MethodPost, "/email/resend", cookie, ""); status != http.StatusConflict {
		t.Errorf("resend when confirmed: %d", status)
	}
	// an address another user has: refused at once
	createUsers(t, db, "password", "yann")
	if status, _ := rawRequest(client, http.MethodPost, "/user", cookie,
		`{"oldpassword": "password", "email": "YANN@test.local"}`); status != http.StatusConflict {
		t.Errorf("taken address: %d", status)
	}
}

func TestEmailChangeWithoutMail(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	client, _ := emailRouter(t, db, nil)
	createUsers(t, db, "password", "zoe")
	now := time.Now()
	db.Model(&models.Users{}).Where("username = ?", "zoe").Update("email_verified_at", now)
	cookie := client.loginCookie("zoe", "password")
	if status, _ := rawRequest(client, http.MethodPost, "/user", cookie, `{"oldpassword": "password", "email": "z@new.example"}`); status != http.StatusOK {
		t.Fatalf("change: %d", status)
	}
	var user models.Users
	db.Where("username = ?", "zoe").First(&user)
	if user.Email != "z@new.example" || user.EmailVerifiedAt != nil {
		t.Errorf("without emails: %s, confirmed %v", user.Email, user.EmailVerifiedAt)
	}
	if status, _, _ := client.browser(http.MethodPost, "/forgot", nil, `{"login": "zoe"}`); status != http.StatusConflict {
		t.Errorf("forgot without emails: %d", status)
	}
}

func TestForgotPassword(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	vars.Domain = "https://sport.example.com"
	mailer := &recordingMailer{}
	client, App := emailRouter(t, db, mailer)
	createUsers(t, db, "password", "zoe", "unconfirmed")
	db.Model(&models.Users{}).Where("username = ?", "zoe").Update("email_verified_at", time.Now())

	forgot := func(login string) map[string]string {
		t.Helper()
		status, answer, _ := client.browser(http.MethodPost, "/forgot", nil, `{"login": "`+login+`"}`)
		if status != http.StatusOK {
			t.Fatalf("%s: %d %v", login, status, answer)
		}
		App.Wait(5 * time.Second)
		return answer
	}
	// the same answer for anyone
	same := forgot("nobody")["message"]
	if forgot("unconfirmed")["message"] != same || forgot("ZOE@test.local")["message"] != same {
		t.Error("the answers differ")
	}
	sent := mailer.messages()
	if len(sent) != 1 || sent[0].To.Address != "zoe@test.local" {
		t.Fatalf("sent %v", sent)
	}
	page, code := linkCode(t, sent[0])
	if page != "reset-password" {
		t.Errorf("page %s", page)
	}
	if status, _, _ := client.browser(http.MethodPost, "/password-reset", nil,
		`{"code": "`+code+`", "password": "brand new"}`); status != http.StatusOK {
		t.Fatalf("reset: %d", status)
	}
	client.loginCookie("zoe", "brand new")
	// at most 3 an hour per account, the answer unchanged
	forgot("zoe")
	forgot("zoe")
	forgot("zoe")
	if n := len(mailer.messages()); n != 3 {
		t.Errorf("%d emails, want 3: the fourth is limited", n)
	}
}

func TestInvitationAndTestEmails(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	vars.Domain = "https://sport.example.com"
	mailer := &recordingMailer{}
	client, _ := emailRouter(t, db, mailer)
	createUsers(t, db, "password", "boss")
	db.Model(&models.Users{}).Where("username = ?", "boss").Update("role", models.RoleAdmin)
	admin := client.loginCookieMFA(db, "boss", "password", enableTOTP(t, db, "boss"))

	if status, _ := rawRequest(client, http.MethodPost, "/admin/invitations", admin, `{"send": true}`); status != http.StatusBadRequest {
		t.Errorf("send without email: %d", status)
	}
	status, body := rawRequest(client, http.MethodPost, "/admin/invitations", admin, `{"email": "new@example.org", "send": true}`)
	answer := jsonAnswer(t, body)
	sent := mailer.messages()
	if status != http.StatusCreated || answer["email_sent"] != true || len(sent) != 1 || sent[0].To.Address != "new@example.org" ||
		!strings.Contains(sent[0].Text, answer["link"].(string)) || !strings.Contains(sent[0].Subject, "boss") {
		t.Fatalf("invitation: %d %v %v", status, answer, sent)
	}

	if status, body := rawRequest(client, http.MethodPost, "/admin/email/test", admin, ""); status != http.StatusOK ||
		!strings.Contains(string(body), "boss@test.local") {
		t.Errorf("test email: %d %s", status, body)
	}
	// the server fails: the admin reads why; an invitation is still created, its link to give by hand
	mailer.fail = true
	if status, body := rawRequest(client, http.MethodPost, "/admin/email/test", admin, ""); status != http.StatusBadGateway ||
		!strings.Contains(string(body), "connection refused") {
		t.Errorf("failing test email: %d %s", status, body)
	}
	status, body = rawRequest(client, http.MethodPost, "/admin/invitations", admin, `{"email": "other@example.org", "send": true}`)
	if answer := jsonAnswer(t, body); status != http.StatusCreated || answer["email_sent"] != false || answer["link"] == "" {
		t.Errorf("invitation with a failing server: %d %v", status, answer)
	}
}

func TestEmailNotifications(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	vars.Domain = "https://sport.example.com"
	mailer := &recordingMailer{}
	client, App := emailRouter(t, db, mailer)
	users := createUsers(t, db, "password", "zoe")
	cookie := client.loginCookie("zoe", "password")

	if status, _ := rawRequest(client, http.MethodPost, "/notifications/email", cookie, `{"enabled": true}`); status != http.StatusConflict {
		t.Errorf("enable unconfirmed: %d", status)
	}
	db.Model(&models.Users{}).Where("id = ?", users[0].ID).Update("email_verified_at", time.Now())
	if status, _ := rawRequest(client, http.MethodPost, "/notifications/email", cookie, `{"enabled": true}`); status != http.StatusOK {
		t.Fatalf("enable: %d", status)
	}
	status, body := rawRequest(client, http.MethodGet, "/notifications/email", cookie, "")
	if status != http.StatusOK || jsonAnswer(t, body)["enabled"] != true {
		t.Errorf("status: %d %s", status, body)
	}

	// a notification of the app goes by email, with the link of its page
	message := notifications.PushMessage{Title: "Service due: bike", Body: "1012 km since the last service",
		URL: "/#/equipment/x/edit#services"}.Encode()
	if queued, err := db.NotifyUser(users[0].ID, message); err != nil || queued != 1 {
		t.Fatalf("queued %d %v", queued, err)
	}
	App.Notifications.Email = App.EmailNotification
	pending, err := db.GetUnsentNotifications()
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending %v %v", pending, err)
	}
	if err := App.Notifications.Notify(pending[0]); err != nil {
		t.Fatal(err)
	}
	sent := mailer.messages()
	if len(sent) != 1 || sent[0].Subject != "Service due: bike" ||
		!strings.Contains(sent[0].Text, "https://sport.example.com/#/equipment/x/edit#services") ||
		!strings.Contains(sent[0].Text, "/#/profile/view") {
		t.Fatalf("sent %v", sent)
	}
	// off: nothing queued for it anymore
	rawRequest(client, http.MethodPost, "/notifications/email", cookie, `{"enabled": false}`)
	if queued, _ := db.NotifyUser(users[0].ID, message); queued != 0 {
		t.Errorf("queued after turning it off: %d", queued)
	}
}
