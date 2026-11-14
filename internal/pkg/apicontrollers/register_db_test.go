package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/models"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

func registerRouter(db *models.DB) sessionClient {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	App := &app.App{DB: *db, Logger: zerolog.Nop()}
	router.Use(func(c *gin.Context) { c.Set("App", App); c.Next() })
	router.POST("/register", Register)
	return sessionClient{router: router}
}

func register(client sessionClient, username, email, password string) (int, map[string]string) {
	return client.send(http.MethodPost, "/register", "",
		fmt.Sprintf(`{"username": %q, "email": %q, "password": %q}`, username, email, password))
}

func role(t *testing.T, db *models.DB, username string) string {
	var user models.Users
	if err := db.Unscoped().Where("username = ?", username).First(&user).Error; err != nil {
		t.Fatalf("%s: %v", username, err)
	}
	return user.Role
}

func TestRegister(t *testing.T) {
	db := testDB(t)
	client := registerRouter(db)

	if code, answer := register(client, "first", "first@test.local", "password"); code != http.StatusCreated {
		t.Fatalf("first registration: %d %v", code, answer)
	}
	if code, answer := register(client, "second", "second@test.local", "password"); code != http.StatusCreated {
		t.Fatalf("second registration: %d %v", code, answer)
	}
	if role(t, db, "first") != "Admin" || role(t, db, "second") != "User" {
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
		{"taken username", "first", "other@test.local", "password", http.StatusConflict},
		// was answered 201 without creating anything
		{"taken email", "other", "first@test.local", "password", http.StatusConflict},
		{"taken email, other case", "other", "First@Test.local", "password", http.StatusConflict},
		{"username of a deleted user", "second", "other@test.local", "password", http.StatusConflict},
		{"email of a deleted user", "other", "second@test.local", "password", http.StatusConflict},
		{"invalid email", "other", "not an email", "password", http.StatusBadRequest},
		{"email with a name", "other", "Other <other@test.local>", "password", http.StatusBadRequest},
		{"missing password", "other", "other@test.local", "", http.StatusBadRequest},
		{"blank username", "  ", "other@test.local", "password", http.StatusBadRequest},
		{"password too long for bcrypt", "other", "other@test.local", strings.Repeat("x", 73), http.StatusBadRequest},
	} {
		if code, answer := register(client, tt.username, tt.email, tt.password); code != tt.want {
			t.Errorf("%s: %d %v, want %d", tt.name, code, answer, tt.want)
		}
	}
	var count int64
	db.Unscoped().Model(&models.Users{}).Count(&count)
	if count != 2 {
		t.Errorf("%d users, want the first two only", count)
	}
}

// simultaneous first registrations: one admin only
func TestRegisterOneFirstAdmin(t *testing.T) {
	db := testDB(t)
	client := registerRouter(db)

	const n = 4
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _ = register(client, fmt.Sprintf("user%d", i), fmt.Sprintf("user%d@test.local", i), "password")
		}()
	}
	wg.Wait()
	for i, code := range codes {
		if code != http.StatusCreated {
			t.Errorf("user%d: %d", i, code)
		}
	}
	var admins int64
	if err := db.Model(&models.Users{}).Where("role = ?", "Admin").Count(&admins).Error; err != nil {
		t.Fatal(err)
	}
	if admins != 1 {
		t.Errorf("%d admins, want 1", admins)
	}
}
