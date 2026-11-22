package notifications

import (
	"MySportWeb/internal/pkg/models"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func testDB(t *testing.T) *models.DB {
	t.Helper()
	dsn := os.Getenv("MYSPORTWEB_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSPORTWEB_TEST_DSN is not set")
	}
	db, err := models.ConnectToDB("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	var name string
	if err := db.Raw("SELECT current_database()").Scan(&name).Error; err != nil || !strings.Contains(name, "test") {
		t.Fatalf("refusing to empty database %q (%v), its name must contain \"test\"", name, err)
	}
	if err := models.CreateOrMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("TRUNCATE notifications, channels, services, equipments, activities, users RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	return db
}

// subscriptionConfig returns the PushSubscription of a browser, with real keys: the payload is encrypted
// with them
func subscriptionConfig(t *testing.T, endpoint string) string {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	config, _ := json.Marshal(webpush.Subscription{Endpoint: endpoint, Keys: webpush.Keys{
		P256dh: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		Auth:   base64.RawURLEncoding.EncodeToString(auth),
	}})
	return string(config)
}

// a push service answering each subscription (by its path) with a status
type pushService struct {
	*httptest.Server
	mu       sync.Mutex
	status   map[string]int
	requests []*http.Request
}

func newPushService(t *testing.T) *pushService {
	s := &pushService{status: map[string]int{}}
	s.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.requests = append(s.requests, r)
		status, ok := s.status[r.URL.Path]
		if !ok {
			status = http.StatusCreated
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(s.Close)
	saved := pushClient
	pushClient = s.Client()
	t.Cleanup(func() { pushClient = saved })
	return s
}

func TestSend(t *testing.T) {
	db := testDB(t)
	service := newPushService(t)
	user := models.Users{Username: "push", Email: "push@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	channel := func(path string) uint {
		c, err := db.SavePushChannel(user.ID, service.URL+path, subscriptionConfig(t, service.URL+path), path)
		if err != nil {
			t.Fatal(err)
		}
		return c.ID
	}
	ok, gone, failing := channel("/ok"), channel("/gone"), channel("/failing")
	service.status["/gone"] = http.StatusGone
	service.status["/failing"] = http.StatusInternalServerError
	if n, err := db.NotifyUser(user.ID, PushMessage{Title: "t", Body: "b", URL: "/#/"}.Encode()); err != nil || n != 3 {
		t.Fatalf("queued %d (%v), want 3", n, err)
	}

	keys, err := (&Notifs{}).GenerateVAPIDKeys(zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	notif := &Notifs{PubKey: keys.PubKey, PrivKey: keys.PrivKey, Subject: "https://sport.example.com"}
	notif.send(zerolog.New(zerolog.NewTestWriter(t)), *db)

	if len(service.requests) != 3 {
		t.Fatalf("%d requests to the push service", len(service.requests))
	}
	for _, r := range service.requests {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "vapid t=") || r.Header.Get("Content-Encoding") != "aes128gcm" {
			t.Errorf("%s: Authorization %q, Content-Encoding %q", r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Encoding"))
		}
	}
	unsent := func(channel uint) int64 {
		var n int64
		db.Model(&models.Notifications{}).Where("channel_id = ? AND sent = ?", channel, false).Count(&n)
		return n
	}
	if unsent(ok) != 0 {
		t.Error("the sent notification is still unsent")
	}
	if unsent(failing) != 1 {
		t.Error("the failed notification is not kept for the next round")
	}
	var channels int64
	db.Unscoped().Model(&models.Channel{}).Where("id = ?", gone).Count(&channels)
	if channels != 0 || unsent(gone) != 0 {
		t.Errorf("the gone subscription: %d channel, %d notifications left", channels, unsent(gone))
	}

	// a day later, still failing: dropped
	db.Model(&models.Notifications{}).Where("channel_id = ?", failing).Update("created_at", time.Now().Add(-StaleAfter-time.Minute))
	notif.send(zerolog.Nop(), *db)
	if unsent(failing) != 0 {
		t.Error("a notification failing for a day is not dropped")
	}
}

// the reminders due are notified once per cycle, grouped per equipment, the time ones from 08:00 in the
// user's timezone and, only when used, after an activity; never without subscription
func TestMaintenanceReminders(t *testing.T) {
	db := testDB(t)
	paris, _ := time.LoadLocation("Europe/Paris")
	at := func(month time.Month, day, hour, minute int) time.Time {
		return time.Date(2026, month, day, hour, minute, 0, 0, paris)
	}
	user := models.Users{Username: "gear", Email: "gear@test.local", Timezone: "Europe/Paris"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	equipment := func(name string) models.Equipments {
		e := models.Equipments{Name: name, UserID: int(user.ID)}
		if err := db.Omit("User").Create(&e).Error; err != nil {
			t.Fatal(err)
		}
		return e
	}
	bike, road := equipment("Gravel"), equipment("Road")
	reminder := func(e models.Equipments, r models.MaintenanceReminder) models.MaintenanceReminder {
		r.EquipmentID = e.ID
		r.OnlyWhenUsed = true
		if r.StartsAt.IsZero() {
			r.StartsAt = at(time.January, 31, 10, 0)
		}
		if err := db.CreateReminder(&r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	service := reminder(bike, models.MaintenanceReminder{Name: "Service", EveryKm: 100, StartsAt: time.Unix(0, 0)})
	reminder(bike, models.MaintenanceReminder{Name: "Clean the transmission", EveryCount: 1, EveryUnit: models.UnitMonth})
	pressure := reminder(bike, models.MaintenanceReminder{Name: "Check the tire pressure", EveryCount: 1, EveryUnit: models.UnitWeek})
	reminder(road, models.MaintenanceReminder{Name: "Check the tire pressure", EveryCount: 1, EveryUnit: models.UnitWeek})
	ride := func(km float64, date time.Time) {
		a := models.Activity{ID: uuid.New(), UserID: user.ID, EquipmentID: &bike.ID, Distance: km * 1000, Duration: 3600,
			Date: date, Sport: "cycling"}
		if err := db.Omit("User", "Equipment").Create(&a).Error; err != nil {
			t.Fatal(err)
		}
	}
	done := func(date time.Time, reminders ...uuid.UUID) {
		if err := db.CreateService(&models.Services{EquipmentID: bike.ID, Date: date, ReminderIDs: reminders}); err != nil {
			t.Fatal(err)
		}
	}
	queued := func() []PushMessage {
		var raw []string
		db.Model(&models.Notifications{}).Order("id").Pluck("message", &raw)
		messages := make([]PushMessage, len(raw))
		for i, m := range raw {
			if err := json.Unmarshal([]byte(m), &messages[i]); err != nil {
				t.Fatal(err)
			}
		}
		return messages
	}
	check := func(now time.Time) {
		t.Helper()
		if err := CheckServices(*db, zerolog.Nop(), now); err != nil {
			t.Fatal(err)
		}
	}
	ride(120, at(time.February, 1, 9, 0))

	// past its km, but nobody to tell: nothing recorded, told once subscribed
	check(at(time.February, 1, 12, 0))
	if len(queued()) != 0 {
		t.Fatal("a notification without subscription")
	}
	if _, err := db.SavePushChannel(user.ID, "https://fcm.googleapis.com/fcm/send/x", subscriptionConfig(t, "https://fcm.googleapis.com/fcm/send/x"), "phone"); err != nil {
		t.Fatal(err)
	}
	check(at(time.February, 1, 12, 0))
	check(at(time.February, 1, 12, 15))
	messages := queued()
	if len(messages) != 1 || messages[0].Title != "Service: Gravel" ||
		messages[0].URL != "/#/equipment/"+bike.ID.String()+"/edit#reminders" ||
		messages[0].Body != "Service: 120 km so far (every 100 km)." {
		t.Fatalf("km reminder: %+v", messages)
	}

	// the weekly one is due on February 7, notified from 08:00 in Paris
	check(at(time.February, 7, 7, 45))
	if len(queued()) != 1 {
		t.Fatal("a time reminder notified before 08:00")
	}
	check(at(time.February, 7, 8, 0))
	if messages = queued(); len(messages) != 2 || messages[1].Title != "Check the tire pressure: Gravel" ||
		messages[1].Body != "Check the tire pressure: due since February 7 (every week)." {
		t.Fatalf("weekly reminder: %+v", messages[1:])
	}

	// the pressure checked on the 21st, the bike ridden: due again on the 28th, with the monthly cleaning
	// (January 31 + 1 month): one notification for both
	done(at(time.February, 21, 10, 0), pressure.ID)
	ride(20, at(time.February, 22, 10, 0))
	check(at(time.February, 27, 20, 0))
	if len(queued()) != 2 {
		t.Fatal("notified before the due day")
	}
	check(at(time.February, 28, 9, 0))
	if messages = queued(); len(messages) != 3 || messages[2].Title != "Maintenance due: Gravel" ||
		!strings.Contains(messages[2].Body, "Clean the transmission: due since February 28 (every month).") ||
		!strings.Contains(messages[2].Body, "Check the tire pressure: due since February 28 (every week).") {
		t.Fatalf("grouped: %+v", messages[2:])
	}

	// serviced: a new cycle, notified again once past its km
	done(at(time.March, 1, 10, 0), service.ID)
	ride(60, at(time.March, 2, 10, 0))
	check(at(time.March, 2, 12, 0))
	if len(queued()) != 3 {
		t.Fatal("notified before the km of the new cycle")
	}
	ride(50, at(time.March, 3, 10, 0))
	check(at(time.March, 3, 12, 0))
	check(at(time.March, 3, 12, 15))
	if messages = queued(); len(messages) != 4 || messages[3].Body != "Service: 110 km since it was last done (every 100 km)." {
		t.Fatalf("after the service: %+v", messages[3:])
	}

	// the road bike never ridden: its weekly reminder waits for a ride
	for _, m := range queued() {
		if strings.Contains(m.Title, "Road") {
			t.Errorf("an unused equipment notified: %+v", m)
		}
	}
	reminders, err := db.GetEquipmentReminders(road.ID)
	if err != nil || len(reminders) != 1 || !reminders[0].WaitingUse || reminders[0].State != models.ReminderOK {
		t.Errorf("unused: %+v (%v)", reminders, err)
	}
}

// a user removes the subscriptions of their browsers only, with what they still had to send
func TestDeletePushChannel(t *testing.T) {
	db := testDB(t)
	var users []models.Users
	for _, name := range []string{"alice", "bob"} {
		u := models.Users{Username: name, Email: name + "@test.local"}
		if err := db.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
		users = append(users, u)
	}
	endpoint := "https://fcm.googleapis.com/fcm/send/alice"
	first, err := db.SavePushChannel(users[0].ID, endpoint, subscriptionConfig(t, endpoint), "old name")
	if err != nil {
		t.Fatal(err)
	}
	// the same browser subscribing again: the same subscription
	again, err := db.SavePushChannel(users[0].ID, endpoint, subscriptionConfig(t, endpoint), "Firefox on Linux")
	if err != nil || again.ID != first.ID {
		t.Fatalf("subscribing again: %d, want %d (%v)", again.ID, first.ID, err)
	}
	if channels, _ := db.GetPushChannels(users[0].ID); len(channels) != 1 || channels[0].Name != "Firefox on Linux" {
		t.Errorf("subscriptions: %+v", channels)
	}
	if _, err := db.NotifyUser(users[0].ID, "pending"); err != nil {
		t.Fatal(err)
	}
	if found, err := db.DeletePushChannel(users[1].ID, first.ID); err != nil || found {
		t.Errorf("another user removing it: %v %v", found, err)
	}
	if found, err := db.DeletePushChannel(users[0].ID, first.ID); err != nil || !found {
		t.Fatalf("its user removing it: %v %v", found, err)
	}
	var left int64
	db.Unscoped().Model(&models.Notifications{}).Count(&left)
	if channels, _ := db.GetPushChannels(users[0].ID); len(channels) != 0 || left != 0 {
		t.Errorf("after: %d subscriptions, %d notifications", len(channels), left)
	}
}
