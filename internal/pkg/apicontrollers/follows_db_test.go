package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// followsRouter serves the routes of the follows, the profiles and the feed
func followsRouter(db *models.DB) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	App := &app.App{DB: *db, Logger: zerolog.Nop()}
	router.Use(func(c *gin.Context) { c.Set("App", App); c.Next() })
	router.GET("/activity/:id", middlewares.OptionalUser(models.ActivitiesRead), GetActivity)
	router.GET("/users/search", middlewares.IsAuthorized(), SearchUsers)
	router.GET("/users/:uuid", middlewares.IsAuthorized(), GetProfile)
	router.GET("/users/:uuid/activities", middlewares.IsAuthorized(), GetProfileActivities)
	router.POST("/users/:uuid/follow", middlewares.IsAuthorized(), Follow)
	router.DELETE("/users/:uuid/follow", middlewares.IsAuthorized(), Unfollow)
	router.GET("/user/follows", middlewares.IsAuthorized(), GetFollows)
	router.POST("/user/follows/approval", middlewares.IsAuthorized(), SetFollowApproval)
	router.POST("/user/followers/:uuid/accept", middlewares.IsAuthorized(), AcceptFollower)
	router.DELETE("/user/followers/:uuid", middlewares.IsAuthorized(), RemoveFollower)
	router.GET("/feed", middlewares.IsAuthorized(), GetFeed)
	return router
}

// feedActivity creates an activity of a user, at a date, with a visibility; with a public trace when mapped
func feedActivity(t *testing.T, db *models.DB, user models.Users, title string, date time.Time, visibility int,
	mapped bool) string {
	t.Helper()
	a := models.Activity{ID: uuid.New(), UserID: user.ID, Title: title, Sport: "running", Date: date,
		Visibility: visibility, Distance: 10000, Duration: 3000}
	if mapped {
		a.PublicGpsPoints = types.GpsPointArray{{Lat: 45, Lon: 5}, {Lat: 45.01, Lon: 5.01}}
	}
	if err := db.Omit("User", "Equipment").Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	return a.ID.String()
}

// titles returns the titles of the activities of a page of the feed, and its next cursor
func titles(answer map[string]interface{}) ([]string, map[string]interface{}) {
	var list []string
	activities, _ := answer["activities"].([]interface{})
	for _, a := range activities {
		list = append(list, a.(map[string]interface{})["title"].(string))
	}
	next, _ := answer["next"].(map[string]interface{})
	return list, next
}

func TestFollowWithApproval(t *testing.T) {
	db := testDB(t)
	router := followsRouter(db)
	users, clients := socialUsers(t, db, router, "admin", "alice", "bob")
	alice, bob := clients[1], clients[2]
	aliceUUID, bobUUID := users[1].UUID.String(), users[2].UUID.String()
	if err := db.SetEmailChannel(users[1].ID, true); err != nil {
		t.Fatal(err)
	}
	if err := db.SetEmailChannel(users[2].ID, true); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	feedActivity(t, db, users[1], "private", now.Add(-time.Hour), visibilityPrivate, false)
	feedActivity(t, db, users[1], "users", now.Add(-2*time.Hour), visibilityUsers, true)
	feedActivity(t, db, users[1], "public", now.Add(-3*time.Hour), visibilityPublic, false)
	feedActivity(t, db, users[1], "older", now.Add(-4*time.Hour), visibilityPublic, false)

	// a request: alice is asked, bob's feed is empty
	if code, answer := bob.send(http.MethodPost, "/users/"+aliceUUID+"/follow", ""); code != http.StatusOK ||
		answer["state"] != models.FollowPending {
		t.Fatalf("bob follows: %d %v", code, answer)
	}
	if code, answer := bob.send(http.MethodPost, "/users/"+aliceUUID+"/follow", ""); code != http.StatusOK ||
		answer["state"] != models.FollowPending {
		t.Errorf("bob follows again: %d %v", code, answer)
	}
	if q := queuedFor(t, db, users[1].ID); len(q) != 1 || !strings.Contains(q[0], "bob asks to follow you") {
		t.Errorf("alice notified: %q", q)
	}
	if _, answer := bob.send(http.MethodGet, "/feed", ""); len(answer["activities"].([]interface{})) != 0 {
		t.Errorf("feed before accepted: %v", answer)
	}
	_, answer := alice.send(http.MethodGet, "/user/follows", "")
	followers := answer["followers"].([]interface{})
	if len(followers) != 1 || followers[0].(map[string]interface{})["username"] != "bob" ||
		followers[0].(map[string]interface{})["accepted_at"] != nil || answer["approval"] != true {
		t.Errorf("alice's followers: %v", answer)
	}

	// accepted: bob is told, his feed has alice's activities but the private one, page by page
	if code, answer := alice.send(http.MethodPost, "/user/followers/"+bobUUID+"/accept", ""); code != http.StatusOK {
		t.Fatalf("accept: %d %v", code, answer)
	}
	if code, _ := alice.send(http.MethodPost, "/user/followers/"+bobUUID+"/accept", ""); code != http.StatusNotFound {
		t.Errorf("accept twice: %d", code)
	}
	if q := queuedFor(t, db, users[2].ID); len(q) != 1 || !strings.Contains(q[0], "alice accepted your request") {
		t.Errorf("bob notified: %q", q)
	}
	code, answer := bob.send(http.MethodGet, "/feed?limit=2", "")
	page, next := titles(answer)
	if code != http.StatusOK || fmt.Sprint(page) != "[users public]" || next == nil {
		t.Fatalf("feed page 1: %d %v", code, answer)
	}
	first := answer["activities"].([]interface{})[0].(map[string]interface{})
	if !strings.Contains(first["thumbnail"].(string), aliceUUID) ||
		first["owner"].(map[string]interface{})["username"] != "alice" {
		t.Errorf("feed item: %v", first)
	}
	// the trace never in the feed
	if raw, _ := json.Marshal(first); strings.Contains(string(raw), "gps") {
		t.Errorf("trace in the feed: %s", raw)
	}
	query := url.Values{"limit": {"2"}, "before": {next["before"].(string)}, "before_id": {next["before_id"].(string)}}
	_, answer = bob.send(http.MethodGet, "/feed?"+query.Encode(), "")
	if page, next := titles(answer); fmt.Sprint(page) != "[older]" || next != nil {
		t.Errorf("feed page 2: %v", answer)
	}
	for _, q := range []string{"limit=0", "limit=51", "before=yesterday&before_id=" + uuid.NewString(),
		"before=" + url.QueryEscape(now.Format(time.RFC3339)) + "&before_id=nope"} {
		if code, _ := bob.send(http.MethodGet, "/feed?"+q, ""); code != http.StatusBadRequest {
			t.Errorf("%s: %d", q, code)
		}
	}

	// the profiles: the counts, the follow, the activities each one may see
	code, answer = bob.send(http.MethodGet, "/users/"+aliceUUID, "")
	if code != http.StatusOK || answer["state"] != models.FollowFollowing || answer["follows_you"] != false ||
		answer["me"] != false || fmt.Sprint(answer["counts"]) != "map[followers:1 following:0]" {
		t.Errorf("alice's profile for bob: %d %v", code, answer)
	}
	if _, answer := alice.send(http.MethodGet, "/users/"+bobUUID, ""); answer["follows_you"] != true ||
		answer["state"] != models.FollowNone {
		t.Errorf("bob's profile for alice: %v", answer)
	}
	if _, answer := alice.send(http.MethodGet, "/users/"+aliceUUID, ""); answer["me"] != true {
		t.Errorf("her own profile: %v", answer)
	}
	if _, answer := bob.send(http.MethodGet, "/users/"+aliceUUID+"/activities", ""); fmt.Sprint(titles(answer)) != "[users public older] map[]" {
		t.Errorf("alice's activities for bob: %v", answer)
	}
	if _, answer := alice.send(http.MethodGet, "/users/"+aliceUUID+"/activities", ""); len(answer["activities"].([]interface{})) != 4 {
		t.Errorf("alice's activities for her: %v", answer)
	}

	// removed: out of bob's feed, not told
	if code, _ := alice.send(http.MethodDelete, "/user/followers/"+bobUUID, ""); code != http.StatusOK {
		t.Errorf("remove: %d", code)
	}
	if _, answer := bob.send(http.MethodGet, "/feed", ""); len(answer["activities"].([]interface{})) != 0 {
		t.Errorf("feed after removed: %v", answer)
	}
	if q := queuedFor(t, db, users[2].ID); len(q) != 1 {
		t.Errorf("bob told of the removal: %q", q)
	}

	// turning the approval off accepts the requests waiting
	bob.send(http.MethodPost, "/users/"+aliceUUID+"/follow", "")
	if code, answer := alice.send(http.MethodPost, "/user/follows/approval", `{"enabled": false}`); code != http.StatusOK ||
		answer["approval"] != false || answer["followers"].([]interface{})[0].(map[string]interface{})["accepted_at"] == nil {
		t.Errorf("approval off: %d %v", code, answer)
	}

	// unfollow
	if code, answer := bob.send(http.MethodDelete, "/users/"+aliceUUID+"/follow", ""); code != http.StatusOK ||
		answer["state"] != models.FollowNone {
		t.Errorf("unfollow: %d %v", code, answer)
	}
	if _, answer := alice.send(http.MethodGet, "/user/follows", ""); len(answer["followers"].([]interface{})) != 0 {
		t.Errorf("after unfollow: %v", answer)
	}
}

func TestFollowWithoutApproval(t *testing.T) {
	db := testDB(t)
	router := followsRouter(db)
	users, clients := socialUsers(t, db, router, "admin", "alice", "bob", "carol")
	alice, bob := clients[1], clients[2]
	aliceUUID := users[1].UUID.String()
	if err := db.SetFollowApproval(users[1].ID, false); err != nil {
		t.Fatal(err)
	}
	if err := db.SetEmailChannel(users[1].ID, true); err != nil {
		t.Fatal(err)
	}
	feedActivity(t, db, users[1], "run", time.Now(), visibilityUsers, false)
	feedActivity(t, db, users[3], "carol's run", time.Now(), visibilityPublic, false)

	if code, answer := bob.send(http.MethodPost, "/users/"+aliceUUID+"/follow", ""); code != http.StatusOK ||
		answer["state"] != models.FollowFollowing {
		t.Fatalf("bob follows: %d %v", code, answer)
	}
	if q := queuedFor(t, db, users[1].ID); len(q) != 1 || !strings.Contains(q[0], "bob follows you") {
		t.Errorf("alice notified: %q", q)
	}
	// only the users followed
	if _, answer := bob.send(http.MethodGet, "/feed", ""); fmt.Sprint(titles(answer)) != "[run] map[]" {
		t.Errorf("feed: %v", answer)
	}

	// alice turns the notifications of the social side off: not told any more
	if err := db.SetNotifySocial(users[1].ID, false); err != nil {
		t.Fatal(err)
	}
	clients[3].send(http.MethodPost, "/users/"+aliceUUID+"/follow", "")
	if q := queuedFor(t, db, users[1].ID); len(q) != 1 {
		t.Errorf("notified while off: %q", q)
	}

	// the refused ones
	if code, _ := alice.send(http.MethodPost, "/users/"+aliceUUID+"/follow", ""); code != http.StatusBadRequest {
		t.Errorf("follows herself: %d", code)
	}
	if code, _ := bob.send(http.MethodPost, "/users/"+uuid.NewString()+"/follow", ""); code != http.StatusNotFound {
		t.Errorf("unknown user: %d", code)
	}
	if code, _ := bob.send(http.MethodGet, "/users/nope", ""); code != http.StatusBadRequest {
		t.Errorf("invalid uuid: %d", code)
	}
	// a disabled user: as unknown, and out of the feed
	now := time.Now()
	db.Model(&models.Users{}).Where("id = ?", users[1].ID).Update("disabled_at", now)
	if code, _ := bob.send(http.MethodGet, "/users/"+aliceUUID, ""); code != http.StatusNotFound {
		t.Errorf("disabled profile: %d", code)
	}
	if _, answer := bob.send(http.MethodGet, "/feed", ""); len(answer["activities"].([]interface{})) != 0 {
		t.Errorf("feed with a disabled user: %v", answer)
	}

	// anonymous: nothing
	req := httptest.NewRequest(http.MethodGet, "/feed", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous feed: %d", w.Code)
	}
}

func TestSearchUsers(t *testing.T) {
	db := testDB(t)
	router := followsRouter(db)
	users, clients := socialUsers(t, db, router, "admin", "Alice", "malice", "bob", "alpha", "a_b", "ally")
	db.Model(&models.Users{}).Where("id = ?", users[6].ID).Update("disabled_at", time.Now())
	if err := db.SetFollowApproval(users[1].ID, false); err != nil {
		t.Fatal(err)
	}
	clients[3].send(http.MethodPost, "/users/"+users[1].UUID.String()+"/follow", "")

	search := func(q string) string {
		t.Helper()
		code, answer := clients[3].send(http.MethodGet, "/users/search?q="+url.QueryEscape(q), "")
		if code != http.StatusOK {
			return fmt.Sprint(code)
		}
		var found []string
		for _, u := range answer["users"].([]interface{}) {
			m := u.(map[string]interface{})
			found = append(found, fmt.Sprintf("%s:%s", m["username"], m["state"]))
			if _, leaked := m["email"]; leaked {
				t.Errorf("email: %v", m)
			}
		}
		return strings.Join(found, " ")
	}
	for q, want := range map[string]string{
		"al":  "Alice:following alpha:none malice:none", // the ones starting with it first; not the disabled ally
		"ALI": "Alice:following malice:none",
		"a_":  "a_b:none", // _ is no wildcard
		"%":   "400",      // too short
		"%%":  "",
		"bob": "", // not the searcher
		"a":   "400",
	} {
		if got := search(q); got != want {
			t.Errorf("%q: %q, want %q", q, got, want)
		}
	}
}

func TestActivityOwner(t *testing.T) {
	db := testDB(t)
	router := followsRouter(db)
	users, clients := socialUsers(t, db, router, "admin", "alice", "bob")
	id := feedActivity(t, db, users[1], "run", time.Now(), visibilityPublic, false)
	_, answer := clients[2].send(http.MethodGet, "/activity/"+id, "")
	if owner, _ := answer["owner"].(map[string]interface{}); owner["username"] != "alice" ||
		owner["uuid"] != users[1].UUID.String() {
		t.Errorf("owner for bob: %v", answer["owner"])
	}
	// the visitors of a public activity aren't told whose it is
	req := httptest.NewRequest(http.MethodGet, "/activity/"+id, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"owner":null`) {
		t.Errorf("anonymous: %d %s", w.Code, w.Body.String()[:min(200, w.Body.Len())])
	}
}
