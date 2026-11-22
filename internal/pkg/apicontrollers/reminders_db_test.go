package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestRemindersAPI(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	gin.SetMode(gin.TestMode)
	router := gin.New()
	App := &app.App{DB: *db, Logger: zerolog.Nop()}
	router.Use(func(c *gin.Context) { c.Set("App", App); c.Next() })
	router.GET("/equipment/:id", middlewares.IsAuthorized(models.EquipmentsRead), GetEquipment)
	router.GET("/equipments", middlewares.IsAuthorized(models.EquipmentsRead), GetEquipments)
	router.GET("/equipment/:id/services", middlewares.IsAuthorized(models.EquipmentsRead), GetServices)
	router.POST("/equipment/:id/services", middlewares.IsAuthorized(models.EquipmentsWrite), CreateService)
	router.POST("/equipment/:id/services/:service", middlewares.IsAuthorized(models.EquipmentsWrite), UpdateService)
	router.GET("/equipment/:id/reminders", middlewares.IsAuthorized(models.EquipmentsRead), GetReminders)
	router.POST("/equipment/:id/reminders", middlewares.IsAuthorized(models.EquipmentsWrite), CreateReminder)
	router.POST("/equipment/:id/reminders/:reminder", middlewares.IsAuthorized(models.EquipmentsWrite), UpdateReminder)
	router.DELETE("/equipment/:id/reminders/:reminder", middlewares.IsAuthorized(models.EquipmentsWrite), DeleteReminder)
	router.POST("/equipment/:id/reminders/:reminder/done", middlewares.IsAuthorized(models.EquipmentsWrite), ReminderDone)
	router.GET("/user/maintenance", middlewares.IsAuthorized(models.EquipmentsRead), UserMaintenance)

	client := func(name string) (models.Users, jsonClient) {
		u := models.Users{Username: name, Email: name + "@test.local", Role: "User"}
		if err := db.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
		db.First(&u, u.ID)
		token, _, err := utils.NewSessionToken(u.ID, u.UUID, u.Role, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return u, jsonClient{router: router, token: token}
	}
	alice, api := client("alice")
	_, mallory := client("mallory")
	bike := models.Equipments{Name: "Gravel", UserID: int(alice.ID), InitialMileage: 1000}
	other := models.Equipments{Name: "Road", UserID: int(alice.ID)}
	for _, e := range []*models.Equipments{&bike, &other} {
		if err := db.CreateEquipment(e); err != nil {
			t.Fatal(err)
		}
	}
	a := models.Activity{ID: uuid.New(), UserID: alice.ID, EquipmentID: &bike.ID, Distance: 2950000, Duration: 7200,
		Date: time.Now().AddDate(0, 0, -1), Sport: "cycling"}
	if err := db.Omit("User", "Equipment").Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	base := "/equipment/" + bike.ID.String() + "/reminders"

	for _, body := range []string{
		`{"name": "", "every_km": 10}`,
		`{"name": "nothing"}`,
		`{"name": "negative", "every_km": -1}`,
		`{"name": "unit", "every_count": 2, "every_unit": "fortnight"}`,
		`{"name": "date", "every_km": 10, "starts_at": "yesterday"}`,
	} {
		if code, answer := api.send(http.MethodPost, base, body); code != http.StatusBadRequest {
			t.Errorf("%s: %d %v", body, code, answer)
		}
	}
	code, chain := api.send(http.MethodPost, base, `{"name": " Change the chain ", "every_km": 3000, "starts_at": "2020-01-01"}`)
	if code != http.StatusCreated || chain["name"] != "Change the chain" || chain["state"] != "soon" ||
		chain["km_left"] != float64(50) || chain["only_when_used"] != true || chain["equipment_name"] != "Gravel" {
		t.Fatalf("create: %d %v", code, chain)
	}
	chainID := fmt.Sprint(chain["id"])
	code, pressure := api.send(http.MethodPost, base, `{"name": "Tire pressure", "every_count": 1, "every_unit": "week", "only_when_used": false, "every_km": 0}`)
	if code != http.StatusCreated || pressure["state"] != "ok" || pressure["only_when_used"] != false || pressure["due_date"] == nil {
		t.Fatalf("create weekly: %d %v", code, pressure)
	}
	pressureID := fmt.Sprint(pressure["id"])

	// someone else's equipment: not found, whatever the reminder
	if code, _ := mallory.send(http.MethodGet, base, ""); code != http.StatusNotFound {
		t.Errorf("another user listing: %d", code)
	}
	if code, _ := mallory.send(http.MethodPost, base+"/"+chainID+"/done", ""); code != http.StatusNotFound {
		t.Errorf("another user marking done: %d", code)
	}
	if code, _ := api.send(http.MethodDelete, "/equipment/"+other.ID.String()+"/reminders/"+chainID, ""); code != http.StatusNotFound {
		t.Errorf("a reminder through another equipment: %d", code)
	}

	// updated: the cycle kept, the interval changed
	code, chain = api.send(http.MethodPost, base+"/"+chainID, `{"name": "Change the chain", "every_km": 2900}`)
	if code != http.StatusOK || chain["state"] != "due" || chain["km_left"] != float64(-50) {
		t.Errorf("update: %d %v", code, chain)
	}
	code, answer := api.send(http.MethodGet, "/user/maintenance", "")
	if list, _ := answer["reminders"].([]interface{}); code != http.StatusOK || len(list) != 1 {
		t.Errorf("maintenance: %d %v", code, answer)
	}
	code, answer = api.send(http.MethodGet, "/equipment/"+bike.ID.String(), "")
	if list, _ := answer["reminders"].([]interface{}); code != http.StatusOK || len(list) != 2 ||
		list[0].(map[string]interface{})["id"] != chainID {
		t.Errorf("equipment: the most urgent first: %d %v", code, answer["reminders"])
	}

	// done: a service at the mileage of the equipment, named after it, a new cycle
	code, answer = api.send(http.MethodPost, base+"/"+chainID+"/done", "")
	service, _ := answer["service"].(map[string]interface{})
	reminder, _ := answer["reminder"].(map[string]interface{})
	if code != http.StatusCreated || service["description"] != "Change the chain" || service["mileage"] != float64(3950) ||
		fmt.Sprint(service["reminder_ids"]) != "["+chainID+"]" || reminder["state"] != "ok" || reminder["km_since"] != float64(0) {
		t.Fatalf("done: %d %v", code, answer)
	}
	code, answer = api.send(http.MethodGet, "/user/maintenance?all=true", "")
	if list, _ := answer["reminders"].([]interface{}); code != http.StatusOK || len(list) != 2 {
		t.Errorf("all: %d %v", code, answer)
	}

	// a service ticking reminders: of its equipment only, kept when not sent on an update
	services := "/equipment/" + bike.ID.String() + "/services"
	code, answer = api.send(http.MethodPost, services, `{"date": "2026-01-10", "description": "full", "reminder_ids": ["`+pressureID+`", "`+chainID+`"]}`)
	if code != http.StatusCreated || len(answer["reminder_ids"].([]interface{})) != 2 {
		t.Fatalf("service: %d %v", code, answer)
	}
	serviceID := fmt.Sprint(answer["id"])
	code, answer = api.send(http.MethodPost, services+"/"+serviceID, `{"date": "2026-01-11", "description": "full service"}`)
	if code != http.StatusOK || len(answer["reminder_ids"].([]interface{})) != 2 {
		t.Errorf("update without reminders: %d %v", code, answer)
	}
	code, answer = api.send(http.MethodPost, services+"/"+serviceID, `{"date": "2026-01-11", "reminder_ids": []}`)
	if code != http.StatusOK || len(answer["reminder_ids"].([]interface{})) != 0 {
		t.Errorf("update unticking: %d %v", code, answer)
	}
	foreign := models.MaintenanceReminder{EquipmentID: other.ID, Name: "x", EveryKm: 1, StartsAt: time.Now()}
	if err := db.CreateReminder(&foreign); err != nil {
		t.Fatal(err)
	}
	if code, answer := api.send(http.MethodPost, services, `{"reminder_ids": ["`+foreign.ID.String()+`"]}`); code != http.StatusBadRequest {
		t.Errorf("a reminder of another equipment: %d %v", code, answer)
	}

	if code, _ := api.send(http.MethodDelete, base+"/"+pressureID, ""); code != http.StatusOK {
		t.Errorf("delete: %d", code)
	}
	if code, _ := api.send(http.MethodDelete, base+"/"+pressureID, ""); code != http.StatusNotFound {
		t.Errorf("deleted twice: %d", code)
	}
}
