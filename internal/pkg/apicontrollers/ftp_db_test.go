package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"MySportWeb/services/activityService"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
)

func TestFTPAPI(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	alice := models.Users{Username: "alice", Email: "alice@test.local", Password: string(hash), Role: "User"}
	if err := db.Create(&alice).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&alice, alice.ID).Error; err != nil {
		t.Fatal(err)
	}
	// a ride of an hour at 250 W, its best 20 minutes 263 W, ten days ago, its metrics computed
	ride := models.Activity{ID: uuid.New(), UserID: alice.ID, Title: "ride", Sport: "cycling",
		Date: time.Now().AddDate(0, 0, -10),
		BestEfforts: types.BestEffortArray{{Sport: "cycling", Kind: types.EffortPower,
			Target: activityService.FTPDuration, Value: 263}}}
	for s := 0; s <= 3600; s++ {
		ride.Powers = append(ride.Powers, 250)
		ride.PowerTS = append(ride.PowerTS, strconv.Itoa(s))
	}
	if err := db.Omit("User", "Equipment").Create(&ride).Error; err != nil {
		t.Fatal(err)
	}
	App := &app.App{DB: *db, Logger: zerolog.Nop()}
	if err := activityService.BackfillPowerMetrics(t.Context(), db, App.Logger); err != nil {
		t.Fatal(err)
	}
	token, _, err := utils.NewSessionToken(alice.ID, alice.UUID, alice.Role, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("App", App); c.Next() })
	router.GET("/user/ftp", middlewares.IsAuthorized(models.ActivitiesRead), FTP)
	router.GET("/user/powerzones", middlewares.IsAuthorized(models.ActivitiesRead), PowerZones)
	router.POST("/user", middlewares.IsAuthorized(), UpdateUser)
	client := jsonClient{router: router, token: token}

	code, answer := client.send(http.MethodGet, "/user/ftp", "")
	estimated, _ := answer["estimated"].(map[string]interface{})
	if code != http.StatusOK || answer["ftp"] != 0.0 || estimated["ftp"] != 250.0 ||
		estimated["activity_id"] != ride.ID.String() {
		t.Fatalf("ftp: %d %v", code, answer)
	}
	code, answer = client.send(http.MethodGet, "/user/powerzones?weeks=4", "")
	if weeks, _ := answer["weeks"].([]interface{}); code != http.StatusOK || len(weeks) != 4 {
		t.Errorf("power zones: %d %v", code, answer)
	}
	if code, _ := client.send(http.MethodGet, "/user/powerzones?weeks=0", ""); code != http.StatusBadRequest {
		t.Errorf("0 weeks: %d", code)
	}

	if code, answer := client.send(http.MethodPost, "/user", `{"oldpassword": "password", "ftp": 3000}`); code != http.StatusBadRequest {
		t.Errorf("FTP 3000: %d %v", code, answer)
	}
	// set: the ride's metrics are computed again with it, in the background
	if code, answer := client.send(http.MethodPost, "/user", `{"oldpassword": "password", "ftp": 200}`); code != http.StatusOK {
		t.Fatalf("FTP 200: %d %v", code, answer)
	}
	if !App.Wait(5 * time.Second) {
		t.Fatal("the recompute didn't end")
	}
	var got models.Activity
	if err := db.First(&got, "id = ?", ride.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.FTP != 200 || got.TSS == nil || fmt.Sprint(*got.TSS) != "156.2" || got.ScoreSource != models.ScorePower {
		t.Errorf("after setting the FTP: ftp %d, tss %v, source %s", got.FTP, got.TSS, got.ScoreSource)
	}
	if code, answer := client.send(http.MethodGet, "/user/ftp", ""); answer["ftp"] != 200.0 {
		t.Errorf("ftp after: %d %v", code, answer)
	}
}
