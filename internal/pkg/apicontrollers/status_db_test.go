package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/metrics"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/status"
	"MySportWeb/internal/pkg/vars"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

// the status of the instance: to the admins logged in with their second factor only
func TestStatusAPI(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	vars.Domain = "https://sport.example.com"
	client := twoFactorRouter(t, db)
	sampler := status.NewSampler(metrics.Registry)
	if err := sampler.Sample(); err != nil {
		t.Fatal(err)
	}
	client.router.GET("/admin/status", middlewares.IsAdmin(), func(c *gin.Context) {
		App := c.MustGet("App").(*app.App)
		App.Status, App.MemoryLimit = sampler, 1<<30
		GetStatus(c)
	})
	users := createUsers(t, db, "password", "boss", "alice")
	db.Model(&models.Users{}).Where("id = ?", users[0].ID).Update("role", models.RoleAdmin)
	secret := enableTOTP(t, db, "boss")
	admin := client.loginCookieMFA(db, "boss", "password", secret)

	status, answer := adminRequest(client, http.MethodGet, "/admin/status", admin, "", "")
	if status != http.StatusOK {
		t.Fatalf("admin: %d %v", status, answer)
	}
	checks, _ := answer["checks"].([]interface{})
	memory, _ := answer["memory"].(map[string]interface{})
	if len(checks) == 0 || memory["limit"] != float64(1<<30) || answer["history"] == nil || answer["last_hour"] == nil {
		t.Errorf("report: %d checks, memory %v, history %v", len(checks), memory, answer["history"])
	}
	if status, _ := adminRequest(client, http.MethodGet, "/admin/status", client.loginCookie("alice", "password"), "", ""); status != http.StatusForbidden {
		t.Errorf("a user: %d", status)
	}
}
