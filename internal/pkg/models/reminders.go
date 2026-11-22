package models

import (
	"errors"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// the units of the time interval of a reminder: calendar ones, a month is the same day of the next month
const (
	UnitDay   = "day"
	UnitWeek  = "week"
	UnitMonth = "month"
	UnitYear  = "year"
)

// ReminderUnits are the units of the time interval of a reminder
var ReminderUnits = []string{UnitDay, UnitWeek, UnitMonth, UnitYear}

// the states of a reminder
const (
	ReminderOK   = "ok"
	ReminderSoon = "soon" // in the last tenth of an interval (a day at least for the time one)
	ReminderDue  = "due"
)

// ReminderNotifyHour is the hour (of the user's timezone) from which a reminder due by its time interval is
// notified: not in the night it becomes due
const ReminderNotifyHour = 8

// ErrUnknownReminder is a reminder that isn't one of the equipment
var ErrUnknownReminder = errors.New("unknown reminder of this equipment")

// MaintenanceReminder is a maintenance to do on an equipment every EveryKm km, EveryHours hours of
// activities, or EveryCount EveryUnit since it was last done (a service done for it), whichever comes first.
// 0 is no such interval, one at least.
type MaintenanceReminder struct {
	ID          uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	EquipmentID uuid.UUID `gorm:"type:uuid;not null" json:"equipment_id"`
	Name        string    `json:"name"`
	EveryKm     int       `json:"every_km"`
	EveryHours  int       `json:"every_hours"`
	EveryCount  int       `json:"every_count"`
	EveryUnit   string    `json:"every_unit"`
	// OnlyWhenUsed: the time interval waits for an activity with the equipment since it was last done
	OnlyWhenUsed bool `json:"only_when_used"`
	// StartsAt starts its cycle until it is done the first time
	StartsAt time.Time `json:"starts_at"`
	// the cycle (date it was last done, nil before the first time) whose notification was sent
	Notified    bool       `json:"-"`
	NotifiedFor *time.Time `json:"-"`
}

// ServiceReminder is a reminder a service did
type ServiceReminder struct {
	ServiceID  uuid.UUID `gorm:"type:uuid;primaryKey"`
	ReminderID uuid.UUID `gorm:"type:uuid;primaryKey"`
}

// ReminderStatus is a reminder with where its cycle is
type ReminderStatus struct {
	MaintenanceReminder
	UserID        uint       `json:"-"`
	Timezone      string     `json:"-"`
	EquipmentName string     `json:"equipment_name"`
	LastDone      *time.Time `json:"last_done"` // the latest service done for it, nil before the first one
	DistanceSince float64    `json:"-"`         // meters of the activities of the cycle
	DurationSince int64      `json:"-"`         // seconds of the activities of the cycle
	UsesSince     int64      `json:"uses_since"`
	KmSince       int        `json:"km_since"`
	HoursSince    float64    `json:"hours_since"`
	KmLeft        *int       `json:"km_left"`    // nil without km interval, negative overdue
	HoursLeft     *float64   `json:"hours_left"` // nil without hours interval, negative overdue
	DueDate       *string    `json:"due_date"`   // YYYY-MM-DD in the user's timezone, nil without time interval
	// WaitingUse: its time interval is reached, it waits for an activity (OnlyWhenUsed)
	WaitingUse bool    `json:"waiting_use"`
	State      string  `json:"state"`
	Progress   float64 `json:"progress"` // of the most advanced interval, 1 when due
	// Notifiable: due, from ReminderNotifyHour for its time interval, and its cycle not notified yet
	Notifiable bool `json:"-"`
}

// AddInterval returns the day (midnight in loc) count units after the day of t: a month later is the same
// day of the month, the last one of a shorter month (January 31 + 1 month is February 28 or 29)
func AddInterval(t time.Time, count int, unit string, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	switch unit {
	case UnitDay:
		return time.Date(y, m, d+count, 0, 0, 0, 0, loc)
	case UnitWeek:
		return time.Date(y, m, d+7*count, 0, 0, 0, 0, loc)
	case UnitMonth:
		return clampedDay(y, m+time.Month(count), d, loc)
	case UnitYear:
		return clampedDay(y+count, m, d, loc)
	}
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// clampedDay is day d of the month (normalised), its last day when it is shorter
func clampedDay(y int, m time.Month, d int, loc *time.Location) time.Time {
	first := time.Date(y, m, 1, 0, 0, 0, 0, loc)
	last := first.AddDate(0, 1, -1).Day()
	if d > last {
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, 0, 0, 0, 0, loc)
}

func locationOf(timezone string) *time.Location {
	if timezone != "" {
		if loc, err := time.LoadLocation(timezone); err == nil {
			return loc
		}
	}
	return time.UTC
}

// evaluate computes the state of the reminder at now
func (s *ReminderStatus) evaluate(now time.Time) {
	loc := locationOf(s.Timezone)
	s.KmSince = int(math.Round(s.DistanceSince / 1000))
	s.HoursSince = math.Round(float64(s.DurationSince)/360) / 10
	s.KmLeft, s.HoursLeft, s.DueDate, s.WaitingUse = nil, nil, nil, false
	anchor := s.StartsAt
	if s.LastDone != nil {
		anchor = *s.LastDone
	}
	var progress float64
	soon, due, notify := false, false, false
	if s.EveryKm > 0 {
		left := s.EveryKm - s.KmSince
		s.KmLeft = &left
		progress = math.Max(progress, float64(s.KmSince)/float64(s.EveryKm))
		if left <= 0 {
			due, notify = true, true
		} else if float64(left) <= float64(s.EveryKm)/10 {
			soon = true
		}
	}
	if s.EveryHours > 0 {
		left := math.Round((float64(s.EveryHours)-s.HoursSince)*10) / 10
		s.HoursLeft = &left
		progress = math.Max(progress, s.HoursSince/float64(s.EveryHours))
		if left <= 0 {
			due, notify = true, true
		} else if left <= float64(s.EveryHours)/10 {
			soon = true
		}
	}
	if s.EveryCount > 0 && s.EveryUnit != "" {
		local := anchor.In(loc)
		start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
		dueDay := AddInterval(anchor, s.EveryCount, s.EveryUnit, loc)
		date := dueDay.Format(time.DateOnly)
		s.DueDate = &date
		total := dueDay.Sub(start)
		margin := total / 10
		if margin < 24*time.Hour {
			margin = 24 * time.Hour
		}
		p := 1.0
		if total > 0 {
			p = float64(now.Sub(start)) / float64(total)
		}
		reached := !now.Before(dueDay)
		if s.OnlyWhenUsed && s.UsesSince == 0 {
			s.WaitingUse = reached
			progress = math.Max(progress, math.Min(p, 1))
		} else {
			progress = math.Max(progress, p)
			if reached {
				due = true
				y, m, d := dueDay.Date()
				if !now.Before(time.Date(y, m, d, ReminderNotifyHour, 0, 0, 0, loc)) {
					notify = true
				}
			} else if !now.Before(dueDay.Add(-margin)) {
				soon = true
			}
		}
	}
	switch {
	case due:
		s.State = ReminderDue
		progress = math.Max(progress, 1)
	case soon:
		s.State = ReminderSoon
	default:
		s.State = ReminderOK
	}
	s.Progress = math.Round(math.Max(progress, 0)*1000) / 1000
	s.Notifiable = notify && !(s.Notified && sameCycle(s.NotifiedFor, s.LastDone))
}

// reminderStatuses returns the reminders of the where clause with their cycle at now, the most urgent first
func (db *DB) reminderStatuses(now time.Time, where string, args ...interface{}) ([]ReminderStatus, error) {
	statuses := []ReminderStatus{}
	err := db.Raw("SELECT r.*, e.user_id, e.name AS equipment_name, u.timezone, l.last_done, "+
		"COALESCE(SUM(a.distance), 0) AS distance_since, COALESCE(SUM(a.duration), 0) AS duration_since, "+
		"COUNT(a.id) AS uses_since "+
		"FROM maintenance_reminders r JOIN equipments e ON e.id = r.equipment_id JOIN users u ON u.id = e.user_id "+
		"LEFT JOIN (SELECT sr.reminder_id, MAX(s.date) AS last_done FROM service_reminders sr "+
		"JOIN services s ON s.id = sr.service_id GROUP BY sr.reminder_id) l ON l.reminder_id = r.id "+
		"LEFT JOIN activities a ON a.equipment_id = r.equipment_id AND a.date > COALESCE(l.last_done, r.starts_at) "+
		"WHERE "+where+" GROUP BY r.id, e.user_id, e.name, u.timezone, l.last_done "+
		"ORDER BY e.name, r.created_at, r.id", args...).Scan(&statuses).Error
	if err != nil {
		return nil, err
	}
	for i := range statuses {
		statuses[i].evaluate(now)
	}
	rank := map[string]int{ReminderDue: 0, ReminderSoon: 1, ReminderOK: 2}
	sort.SliceStable(statuses, func(i, j int) bool {
		a, b := statuses[i], statuses[j]
		if rank[a.State] != rank[b.State] {
			return rank[a.State] < rank[b.State]
		}
		return a.Progress > b.Progress
	})
	return statuses, nil
}

// GetEquipmentReminders returns the reminders of an equipment, the most urgent first
func (db *DB) GetEquipmentReminders(equipmentID uuid.UUID) ([]ReminderStatus, error) {
	return db.reminderStatuses(time.Now(), "r.equipment_id = ?", equipmentID)
}

// GetUserReminders returns the reminders of the equipments of a user, the most urgent first
func (db *DB) GetUserReminders(userID uint) ([]ReminderStatus, error) {
	return db.reminderStatuses(time.Now(), "e.user_id = ?", userID)
}

// GetReminderStatus returns a reminder of an equipment with its cycle, gorm.ErrRecordNotFound when the
// equipment doesn't have it
func (db *DB) GetReminderStatus(equipmentID, id uuid.UUID) (ReminderStatus, error) {
	statuses, err := db.reminderStatuses(time.Now(), "r.equipment_id = ? AND r.id = ?", equipmentID, id)
	if err != nil {
		return ReminderStatus{}, err
	}
	if len(statuses) == 0 {
		return ReminderStatus{}, gorm.ErrRecordNotFound
	}
	return statuses[0], nil
}

// RemindersToNotify returns the reminders to notify at now (Notifiable), of the users with a push
// subscription or the notifications by email: the others are notified once they have one
func (db *DB) RemindersToNotify(now time.Time) ([]ReminderStatus, error) {
	statuses, err := db.reminderStatuses(now, "e.user_id IN (SELECT user_id FROM channels "+
		"WHERE channel_provider IN ? AND deleted_at IS NULL)", notifyingProviders)
	if err != nil {
		return nil, err
	}
	var due []ReminderStatus
	for _, s := range statuses {
		if s.Notifiable {
			due = append(due, s)
		}
	}
	return due, nil
}

// SetReminderNotified records that the cycle lastDone of a reminder was notified
func (db *DB) SetReminderNotified(id uuid.UUID, lastDone *time.Time) error {
	return db.Model(&MaintenanceReminder{}).Where("id = ?", id).
		Updates(map[string]interface{}{"notified": true, "notified_for": lastDone}).Error
}

// GetReminder returns a reminder of an equipment, gorm.ErrRecordNotFound when the equipment doesn't have it
func (db *DB) GetReminder(equipmentID, id uuid.UUID) (MaintenanceReminder, error) {
	var reminder MaintenanceReminder
	err := db.Where("id = ? AND equipment_id = ?", id, equipmentID).First(&reminder).Error
	return reminder, err
}

func (db *DB) CreateReminder(reminder *MaintenanceReminder) error {
	return db.Create(reminder).Error
}

// UpdateReminder saves what the user sets of a reminder; its notification state is kept
func (db *DB) UpdateReminder(reminder *MaintenanceReminder) error {
	return db.Model(reminder).Select("name", "every_km", "every_hours", "every_count", "every_unit",
		"only_when_used", "starts_at", "updated_at").Updates(reminder).Error
}

// DeleteReminder deletes a reminder of an equipment, its services kept; gorm.ErrRecordNotFound when the
// equipment doesn't have it
func (db *DB) DeleteReminder(equipmentID, id uuid.UUID) error {
	result := db.Where("id = ? AND equipment_id = ?", id, equipmentID).Delete(&MaintenanceReminder{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// setServiceReminders makes the reminders of a service ids, reminders of its equipment only
// (ErrUnknownReminder)
func setServiceReminders(tx *gorm.DB, service *Services) error {
	ids := uniqueIDs(service.ReminderIDs)
	if len(ids) > 0 {
		var n int64
		if err := tx.Model(&MaintenanceReminder{}).Where("equipment_id = ? AND id IN ?", service.EquipmentID, ids).
			Count(&n).Error; err != nil {
			return err
		}
		if int(n) != len(ids) {
			return ErrUnknownReminder
		}
	}
	if err := tx.Where("service_id = ?", service.ID).Delete(&ServiceReminder{}).Error; err != nil {
		return err
	}
	links := make([]ServiceReminder, len(ids))
	for i, id := range ids {
		links[i] = ServiceReminder{ServiceID: service.ID, ReminderID: id}
	}
	if len(links) > 0 {
		if err := tx.Create(&links).Error; err != nil {
			return err
		}
	}
	service.ReminderIDs = ids
	return nil
}

func uniqueIDs(ids []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	unique := []uuid.UUID{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	return unique
}

// withReminderIDs fills the reminders the services did
func (db *DB) withReminderIDs(services []Services) error {
	if len(services) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(services))
	for i := range services {
		ids[i] = services[i].ID
		services[i].ReminderIDs = []uuid.UUID{}
	}
	var links []ServiceReminder
	if err := db.Where("service_id IN ?", ids).Order("reminder_id").Find(&links).Error; err != nil {
		return err
	}
	byService := map[uuid.UUID][]uuid.UUID{}
	for _, l := range links {
		byService[l.ServiceID] = append(byService[l.ServiceID], l.ReminderID)
	}
	for i := range services {
		if r, ok := byService[services[i].ID]; ok {
			services[i].ReminderIDs = r
		}
	}
	return nil
}
