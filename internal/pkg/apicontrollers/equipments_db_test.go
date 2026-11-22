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
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

func TestEquipmentDefaultSportsAPI(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	alice := models.Users{Username: "alice", Email: "alice@test.local", Role: "User"}
	if err := db.Create(&alice).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&alice, alice.ID).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := utils.NewSessionToken(alice.ID, alice.UUID, alice.Role, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	App := &app.App{DB: *db, Logger: zerolog.Nop()}
	router.Use(func(c *gin.Context) { c.Set("App", App); c.Next() })
	router.POST("/equipment", middlewares.IsAuthorized(models.EquipmentsCreate), CreateEquipment)
	router.GET("/equipment/:id", middlewares.IsAuthorized(models.EquipmentsRead), GetEquipment)
	router.POST("/equipment/:id", middlewares.IsAuthorized(models.EquipmentsWrite), UpdateEquipment)
	router.GET("/user/sports", middlewares.IsAuthorized(models.ActivitiesRead), UserSports)
	client := jsonClient{router: router, token: token}
	sports := func(answer map[string]interface{}) string { return fmt.Sprint(answer["default_sports"]) }

	code, answer := client.send(http.MethodPost, "/equipment", `{"name": "shoes", "default_sports": ["running", "hiking"]}`)
	if code != http.StatusCreated || sports(answer) != "[hiking running]" {
		t.Fatalf("create: %d %v", code, answer)
	}
	id := fmt.Sprint(answer["id"])

	// a field missing from the request keeps the sports
	code, answer = client.send(http.MethodPost, "/equipment/"+id, `{"name": "trail shoes"}`)
	if code != http.StatusOK || sports(answer) != "[hiking running]" || answer["name"] != "trail shoes" {
		t.Errorf("update without sports: %d %v", code, answer)
	}
	code, answer = client.send(http.MethodGet, "/equipment/"+id, "")
	if code != http.StatusOK || sports(answer) != "[hiking running]" {
		t.Errorf("get: %d %v", code, answer)
	}
	for _, body := range []string{`{"name": "x", "default_sports": ["biking"]}`, `{"name": "x", "default_sports": [""]}`} {
		if code, answer := client.send(http.MethodPost, "/equipment", body); code != http.StatusBadRequest {
			t.Errorf("%s: %d %v", body, code, answer)
		}
		if code, answer := client.send(http.MethodPost, "/equipment/"+id, body); code != http.StatusBadRequest {
			t.Errorf("update %s: %d %v", body, code, answer)
		}
	}

	code, answer = client.send(http.MethodGet, "/user/sports", "")
	all, _ := answer["all"].([]interface{})
	if code != http.StatusOK || fmt.Sprint(answer["sports"]) != "[hiking running]" || len(all) < 10 {
		t.Errorf("sports: %d %v", code, answer)
	}
}

// jsonClient sends the requests of a session, answering the whole JSON object of the answers
type jsonClient struct {
	router *gin.Engine
	token  string
}

func (c jsonClient) send(method, path, body string) (int, map[string]interface{}) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	w := httptest.NewRecorder()
	c.router.ServeHTTP(w, req)
	answer := map[string]interface{}{}
	json.Unmarshal(w.Body.Bytes(), &answer)
	return w.Code, answer
}
