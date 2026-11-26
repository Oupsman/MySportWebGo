package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// socialRouter serves the routes of the cheers and comments, and the list of the activities
func socialRouter(db *models.DB) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	App := &app.App{DB: *db, Logger: zerolog.Nop()}
	router.Use(func(c *gin.Context) { c.Set("App", App); c.Next() })
	router.GET("/activity/list/:start/:count", middlewares.IsAuthorized(models.ActivitiesRead), ListActivities)
	router.GET("/activity/search", middlewares.IsAuthorized(models.ActivitiesRead), SearchActivities)
	router.GET("/activity/:id/social", middlewares.IsAuthorized(), GetActivitySocial)
	router.PUT("/activity/:id/cheer", middlewares.IsAuthorized(), CheerActivity)
	router.DELETE("/activity/:id/cheer", middlewares.IsAuthorized(), UncheerActivity)
	router.POST("/activity/:id/comments", middlewares.IsAuthorized(), CreateComment)
	router.POST("/activity/:id/comments/:comment", middlewares.IsAuthorized(), UpdateComment)
	router.DELETE("/activity/:id/comments/:comment", middlewares.IsAuthorized(), DeleteComment)
	router.GET("/notifications/social", middlewares.IsAuthorized(), SocialNotifications)
	router.POST("/notifications/social", middlewares.IsAuthorized(), SetSocialNotifications)
	return router
}

// socialUsers creates users, the first one an admin, with their session clients
func socialUsers(t *testing.T, db *models.DB, router *gin.Engine, names ...string) ([]models.Users, []jsonClient) {
	t.Helper()
	vars.SecretKey = "testsecret"
	users := make([]models.Users, len(names))
	clients := make([]jsonClient, len(names))
	for i, name := range names {
		role := models.RoleUser
		if i == 0 {
			role = models.RoleAdmin
		}
		u := models.Users{Username: name, Email: name + "@test.local", Role: role}
		if err := db.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
		db.First(&u, u.ID)
		token, _, err := utils.NewSessionToken(u.ID, u.UUID, u.Role, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		users[i], clients[i] = u, jsonClient{router: router, token: token}
		// the ids start again in each test: not the comments of the previous ones
		commentLimiter.Reset(fmt.Sprint(u.ID))
		followLimiter.Reset(fmt.Sprint(u.ID))
	}
	return users, clients
}

// socialActivityOf creates an activity of a user with a visibility
func socialActivityOf(t *testing.T, db *models.DB, user models.Users, visibility int) string {
	t.Helper()
	a := models.Activity{ID: uuid.New(), UserID: user.ID, Title: "Morning ride", Sport: "cycling",
		Date: time.Now(), Visibility: visibility, CanComments: true}
	if err := db.Omit("User", "Equipment").Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	return a.ID.String()
}

// queuedFor returns the messages of the notifications queued for a user
func queuedFor(t *testing.T, db *models.DB, userID uint) []string {
	t.Helper()
	var messages []string
	err := db.Table("notifications n").Joins("JOIN channels c ON c.id = n.channel_id").
		Where("c.user_id = ?", userID).Order("n.id").Pluck("n.message", &messages).Error
	if err != nil {
		t.Fatal(err)
	}
	return messages
}

func TestSocialAccess(t *testing.T) {
	db := testDB(t)
	router := socialRouter(db)
	users, clients := socialUsers(t, db, router, "admin", "owner", "other")
	owner, other := clients[1], clients[2]

	for _, tc := range []struct {
		visibility int
		othersSee  bool
	}{{visibilityPrivate, false}, {visibilityUsers, true}, {visibilityPublic, true}} {
		id := socialActivityOf(t, db, users[1], tc.visibility)
		name := fmt.Sprintf("visibility %d", tc.visibility)

		// the owner sees theirs, but doesn't cheer it
		if code, answer := owner.send(http.MethodGet, "/activity/"+id+"/social", ""); code != http.StatusOK ||
			answer["can_cheer"] != false {
			t.Errorf("%s: owner: %d %v", name, code, answer)
		}
		if code, _ := owner.send(http.MethodPut, "/activity/"+id+"/cheer", ""); code != http.StatusBadRequest {
			t.Errorf("%s: owner cheering: %d", name, code)
		}

		want := http.StatusForbidden
		if tc.othersSee {
			want = http.StatusOK
		}
		for method, path := range map[string]string{http.MethodGet: "/social", http.MethodPut: "/cheer",
			http.MethodDelete: "/cheer"} {
			if code, answer := other.send(method, "/activity/"+id+path, ""); code != want {
				t.Errorf("%s: other %s %s: %d %v", name, method, path, code, answer)
			}
		}
		if code, _ := other.send(http.MethodPost, "/activity/"+id+"/comments", `{"body": "nice"}`); code != want &&
			!(want == http.StatusOK && code == http.StatusCreated) {
			t.Errorf("%s: other commenting: %d", name, code)
		}

		// anonymous: nothing, even of a public activity
		req := httptest.NewRequest(http.MethodGet, "/activity/"+id+"/social", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: anonymous: %d", name, w.Code)
		}
	}

	if code, _ := other.send(http.MethodGet, "/activity/"+uuid.NewString()+"/social", ""); code != http.StatusNotFound {
		t.Errorf("unknown activity: %d", code)
	}
	if code, _ := other.send(http.MethodGet, "/activity/nope/social", ""); code != http.StatusBadRequest {
		t.Errorf("invalid id: %d", code)
	}
}

func TestCheers(t *testing.T) {
	db := testDB(t)
	router := socialRouter(db)
	users, clients := socialUsers(t, db, router, "admin", "owner", "bob", "carol")
	id := socialActivityOf(t, db, users[1], visibilityUsers)

	for i := 0; i < 2; i++ {
		// cheering again changes nothing
		if code, answer := clients[2].send(http.MethodPut, "/activity/"+id+"/cheer", ""); code != http.StatusOK ||
			answer["cheered"] != true || len(answer["cheers"].([]interface{})) != 1 {
			t.Fatalf("bob cheers: %d %v", code, answer)
		}
	}
	clients[3].send(http.MethodPut, "/activity/"+id+"/cheer", "")
	code, answer := clients[1].send(http.MethodGet, "/activity/"+id+"/social", "")
	cheers := answer["cheers"].([]interface{})
	if code != http.StatusOK || answer["cheered"] != false || len(cheers) != 2 {
		t.Fatalf("owner: %d %v", code, answer)
	}
	first := cheers[0].(map[string]interface{})
	if first["username"] != "bob" || first["uuid"] != users[2].UUID.String() {
		t.Errorf("first cheer: %v", first)
	}
	// never the ids nor the emails of the others
	if _, leaked := first["user_id"]; leaked || strings.Contains(fmt.Sprint(answer), "@test.local") {
		t.Errorf("leaked: %v", answer)
	}

	if code, answer := clients[2].send(http.MethodDelete, "/activity/"+id+"/cheer", ""); code != http.StatusOK ||
		answer["cheered"] != false || len(answer["cheers"].([]interface{})) != 1 {
		t.Errorf("bob takes back: %d %v", code, answer)
	}

	// the list of the owner counts them
	code, answer = clients[1].send(http.MethodGet, "/activity/list/0/10", "")
	list := answer["activities"].([]interface{})
	if code != http.StatusOK || len(list) != 1 || list[0].(map[string]interface{})["cheer_count"] != 1.0 {
		t.Errorf("list: %d %v", code, answer)
	}
	code, answer = clients[1].send(http.MethodGet, "/activity/search", "")
	rows := answer["activities"].([]interface{})
	if code != http.StatusOK || len(rows) != 1 || rows[0].(map[string]interface{})["cheer_count"] != 1.0 ||
		rows[0].(map[string]interface{})["comment_count"] != 0.0 {
		t.Errorf("search: %d %v", code, answer)
	}

	// the activity deleted: its cheers too
	if err := db.DeleteActivity(uuid.MustParse(id)); err != nil {
		t.Fatal(err)
	}
	var left int64
	db.Model(&models.Cheer{}).Count(&left)
	if left != 0 {
		t.Errorf("%d cheers left after the activity", left)
	}
}

func TestComments(t *testing.T) {
	db := testDB(t)
	router := socialRouter(db)
	users, clients := socialUsers(t, db, router, "admin", "owner", "bob", "carol")
	admin, owner, bob, carol := clients[0], clients[1], clients[2], clients[3]
	id := socialActivityOf(t, db, users[1], visibilityPublic)
	if err := db.SetEmailChannel(users[1].ID, true); err != nil {
		t.Fatal(err)
	}

	for _, body := range []string{`{"body": ""}`, `{"body": "   "}`, `{"body": "` + strings.Repeat("é", maxCommentLength+1) + `"}`,
		`{"body": "a\u0000b"}`, `{`} {
		if code, answer := bob.send(http.MethodPost, "/activity/"+id+"/comments", body); code != http.StatusBadRequest {
			t.Errorf("%.30s: %d %v", body, code, answer)
		}
	}
	if code, answer := bob.send(http.MethodPost, "/activity/"+id+"/comments",
		`{"body": "`+strings.Repeat("é", maxCommentLength)+`"}`); code != http.StatusCreated {
		t.Errorf("longest comment: %d %v", code, answer)
	}

	code, answer := bob.send(http.MethodPost, "/activity/"+id+"/comments", `{"body": "  Great   ride!\n"}`)
	comments := answer["comments"].([]interface{})
	if code != http.StatusCreated || len(comments) != 2 {
		t.Fatalf("bob comments: %d %v", code, answer)
	}
	comment := comments[1].(map[string]interface{})
	commentID := comment["id"].(string)
	if comment["body"] != "Great   ride!" || comment["mine"] != true || comment["can_delete"] != true ||
		comment["author"].(map[string]interface{})["username"] != "bob" {
		t.Errorf("comment: %v", comment)
	}
	// the owner is notified of the comments of the others, not of theirs
	owner.send(http.MethodPost, "/activity/"+id+"/comments", `{"body": "thanks"}`)
	queued := queuedFor(t, db, users[1].ID)
	if len(queued) != 2 || !strings.Contains(queued[1], "bob commented on Morning ride") ||
		!strings.Contains(queued[1], "Great ride!") {
		t.Errorf("notifications: %q", queued)
	}

	// the others see it, without changing it
	code, answer = carol.send(http.MethodGet, "/activity/"+id+"/social", "")
	seen := answer["comments"].([]interface{})[1].(map[string]interface{})
	if code != http.StatusOK || seen["mine"] != false || seen["can_delete"] != false {
		t.Errorf("carol sees: %d %v", code, seen)
	}
	path := "/activity/" + id + "/comments/" + commentID
	for _, c := range []jsonClient{carol, owner, admin} {
		if code, _ := c.send(http.MethodPost, path, `{"body": "changed"}`); code != http.StatusForbidden {
			t.Errorf("changed by another: %d", code)
		}
	}
	if code, _ := carol.send(http.MethodDelete, path, ""); code != http.StatusForbidden {
		t.Errorf("deleted by carol: %d", code)
	}
	if code, answer := bob.send(http.MethodPost, path, `{"body": "Great ride, really"}`); code != http.StatusOK ||
		answer["comments"].([]interface{})[1].(map[string]interface{})["body"] != "Great ride, really" {
		t.Errorf("bob changes it: %d %v", code, answer)
	}
	// a comment of another activity isn't found through this one
	otherID := socialActivityOf(t, db, users[1], visibilityPublic)
	if code, _ := bob.send(http.MethodDelete, "/activity/"+otherID+"/comments/"+commentID, ""); code != http.StatusNotFound {
		t.Errorf("through another activity: %d", code)
	}

	// deleted by its author, by the owner of the activity, by an admin
	for name, c := range map[string]jsonClient{"author": bob, "owner": owner, "admin": admin} {
		_, answer := bob.send(http.MethodPost, "/activity/"+id+"/comments", `{"body": "to delete"}`)
		all := answer["comments"].([]interface{})
		path := "/activity/" + id + "/comments/" + all[len(all)-1].(map[string]interface{})["id"].(string)
		if code, answer := c.send(http.MethodDelete, path, ""); code != http.StatusOK ||
			len(answer["comments"].([]interface{})) != len(all)-1 {
			t.Errorf("deleted by the %s: %d %v", name, code, answer)
		}
	}

	// closed to comments: none more, the ones there stay, deleted but not changed
	_, answer = bob.send(http.MethodPost, "/activity/"+id+"/comments", `{"body": "before closing"}`)
	all := answer["comments"].([]interface{})
	lastPath := "/activity/" + id + "/comments/" + all[len(all)-1].(map[string]interface{})["id"].(string)
	db.Model(&models.Activity{}).Where("id = ?", id).Update("can_comments", false)
	if code, answer := bob.send(http.MethodPost, "/activity/"+id+"/comments", `{"body": "closed"}`); code != http.StatusForbidden {
		t.Errorf("comment while closed: %d %v", code, answer)
	}
	if code, _ := bob.send(http.MethodPost, lastPath, `{"body": "changed while closed"}`); code != http.StatusForbidden {
		t.Errorf("changed while closed: %d", code)
	}
	if code, answer := carol.send(http.MethodGet, "/activity/"+id+"/social", ""); code != http.StatusOK ||
		answer["can_comment"] != false || len(answer["comments"].([]interface{})) != len(all) {
		t.Errorf("seen while closed: %d %v", code, answer)
	}
	if code, _ := bob.send(http.MethodDelete, lastPath, ""); code != http.StatusOK {
		t.Errorf("deleted while closed: %d", code)
	}
	db.Model(&models.Activity{}).Where("id = ?", id).Update("can_comments", true)

	// the owner turns the notifications off
	if code, answer := owner.send(http.MethodPost, "/notifications/social", `{"enabled": false}`); code != http.StatusOK {
		t.Fatalf("notifications off: %d %v", code, answer)
	}
	if _, answer := owner.send(http.MethodGet, "/notifications/social", ""); answer["enabled"] != false {
		t.Errorf("notifications: %v", answer)
	}
	if code, _ := owner.send(http.MethodPost, "/notifications/social", `{}`); code != http.StatusBadRequest {
		t.Errorf("without enabled: %d", code)
	}
	before := len(queuedFor(t, db, users[1].ID))
	carol.send(http.MethodPost, "/activity/"+id+"/comments", `{"body": "hello"}`)
	if after := len(queuedFor(t, db, users[1].ID)); after != before {
		t.Errorf("notified while off: %d then %d", before, after)
	}

	// the owner's list counts them; deleting the author deletes their comments
	_, answer = owner.send(http.MethodGet, "/activity/list/0/10", "")
	counts := map[string]float64{}
	for _, a := range answer["activities"].([]interface{}) {
		counts[a.(map[string]interface{})["id"].(string)] = a.(map[string]interface{})["comment_count"].(float64)
	}
	if counts[id] != 4 || counts[otherID] != 0 {
		t.Errorf("counts: %v", counts)
	}
	if _, err := db.DeleteUser(users[2].ID); err != nil {
		t.Fatal(err)
	}
	if _, answer := owner.send(http.MethodGet, "/activity/"+id+"/social", ""); len(answer["comments"].([]interface{})) != 2 {
		t.Errorf("after bob's account deleted: %v", answer)
	}
}

func TestCommentsRateLimit(t *testing.T) {
	db := testDB(t)
	router := socialRouter(db)
	users, clients := socialUsers(t, db, router, "admin", "owner", "bob")
	id := socialActivityOf(t, db, users[1], visibilityUsers)
	defer commentLimiter.Reset(fmt.Sprint(users[2].ID))
	for i := 0; i < commentsPerMinute; i++ {
		if code, _ := clients[2].send(http.MethodPost, "/activity/"+id+"/comments", `{"body": "spam"}`); code != http.StatusCreated {
			t.Fatalf("comment %d: %d", i, code)
		}
	}
	if code, _ := clients[2].send(http.MethodPost, "/activity/"+id+"/comments", `{"body": "spam"}`); code != http.StatusTooManyRequests {
		t.Errorf("above the limit: %d", code)
	}
}
