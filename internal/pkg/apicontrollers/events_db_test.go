package apicontrollers

import (
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/vars"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestAdminUserEvents(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	client := twoFactorRouter(t, db)
	r := client.router
	r.GET("/admin/events", middlewares.IsAdmin(), GetUserEvents)
	r.GET("/admin/notifications", middlewares.IsAdmin(), GetNotificationLogs)
	r.GET("/admin/users", middlewares.IsAdmin(), GetUsers)

	users := createUsers(t, db, "password", "boss", "alice")
	boss, alice := users[0], users[1]
	db.Model(&models.Users{}).Where("id = ?", boss.ID).Update("role", models.RoleAdmin)
	secret := enableTOTP(t, db, "boss")
	admin := client.loginCookieMFA(db, "boss", "password", secret)
	aliceCookie := client.loginCookie("alice", "password")
	// a wrong password of alice, and of nobody: only hers is recorded
	for _, name := range []string{"alice", "nobody"} {
		if code, _, _ := client.browser(http.MethodPost, "/login", nil, `{"username": "`+name+`", "password": "wrong"}`); code != http.StatusBadRequest {
			t.Fatalf("wrong password of %s: %d", name, code)
		}
	}

	if status, _ := adminRequest(client, http.MethodGet, "/admin/events", aliceCookie, "", ""); status != http.StatusForbidden {
		t.Errorf("events seen by a user: %d", status)
	}
	events := func(query string) []map[string]interface{} {
		t.Helper()
		status, answer := adminRequest(client, http.MethodGet, "/admin/events"+query, admin, "", "")
		if status != http.StatusOK {
			t.Fatalf("events%s: %d %v", query, status, answer)
		}
		list, _ := answer["events"].([]interface{})
		out := make([]map[string]interface{}, len(list))
		for i, e := range list {
			out[i], _ = e.(map[string]interface{})
		}
		return out
	}
	of := events(fmt.Sprintf("?user_id=%d", alice.ID))
	if len(of) != 2 || of[0]["kind"] != models.EventLoginFailed || of[1]["kind"] != models.EventLogin ||
		of[0]["username"] != "alice" || of[1]["ip"] == "" {
		t.Errorf("events of alice, newest first: %v", of)
	}
	if logins := events("?kind=login"); len(logins) != 2 || logins[1]["detail"] != "password and second factor" {
		t.Errorf("logins: %v", logins)
	}
	if page := events(fmt.Sprintf("?limit=1&before=%v", of[0]["id"])); len(page) != 1 || page[0]["id"] != of[1]["id"] {
		t.Errorf("next page: %v", page)
	}
	for _, query := range []string{"?kind=unknown", "?limit=0", "?user_id=x", "?before=-1"} {
		if status, _ := adminRequest(client, http.MethodGet, "/admin/events"+query, admin, "", ""); status != http.StatusBadRequest {
			t.Errorf("%s: %d", query, status)
		}
	}
	// the last login on the users
	_, answer := adminRequest(client, http.MethodGet, "/admin/users", admin, "", "")
	list, _ := answer["users"].([]interface{})
	for _, u := range list {
		if summary, _ := u.(map[string]interface{}); summary["last_login"] == nil {
			t.Errorf("no last login: %v", summary)
		}
	}

	// dropped after the retention
	old := models.UserEvent{UserID: &alice.ID, Username: "alice", Kind: models.EventUpload,
		CreatedAt: time.Now().Add(-models.EventRetention - time.Hour)}
	if err := db.RecordEvent(&old); err != nil {
		t.Fatal(err)
	}
	if dropped, err := db.DropOldEvents(time.Now()); err != nil || dropped != 1 {
		t.Errorf("dropped %d: %v", dropped, err)
	}

	// the notifications: when, to whom, about what, never the message
	channel := models.Channel{UserID: uint64(alice.ID), ChannelProvider: models.PushProvider, Config: "{}"}
	if err := db.Omit("User").Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := db.NotifyUser(alice.ID, models.NotifyGoal, `{"title": "secret goal"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.NotifyUser(alice.ID, models.NotifyComment, `{"title": "a comment"}`); err != nil {
		t.Fatal(err)
	}
	var first models.Notifications
	if err := db.Where("kind = ?", models.NotifyGoal).First(&first).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.MarkAsSent(first.ID); err != nil {
		t.Fatal(err)
	}
	status, answer := adminRequest(client, http.MethodGet, "/admin/notifications", admin, "", "")
	logs, _ := answer["notifications"].([]interface{})
	if status != http.StatusOK || len(logs) != 2 {
		t.Fatalf("notifications: %d %v", status, answer)
	}
	comment, _ := logs[0].(map[string]interface{})
	goal, _ := logs[1].(map[string]interface{})
	if comment["kind"] != models.NotifyComment || comment["sent"] != false || comment["sent_at"] != nil ||
		comment["username"] != "alice" || comment["provider"] != models.PushProvider {
		t.Errorf("unsent notification: %v", comment)
	}
	if goal["kind"] != models.NotifyGoal || goal["sent"] != true || goal["sent_at"] == nil {
		t.Errorf("sent notification: %v", goal)
	}
	if _, ok := goal["message"]; ok {
		t.Error("the admins see the message")
	}
	status, answer = adminRequest(client, http.MethodGet, "/admin/notifications?kind=goal", admin, "", "")
	if logs, _ := answer["notifications"].([]interface{}); status != http.StatusOK || len(logs) != 1 {
		t.Errorf("goal notifications: %d %v", status, answer)
	}
}
