package models

import (
	"MySportWeb/internal/pkg/types"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The altitude corrections of a device, applied to the activities it recorded
const (
	// AltitudeCorrectionNone keeps the altitudes the device recorded
	AltitudeCorrectionNone = "none"
	// AltitudeCorrectionReplace replaces them with the elevation of the ground under the GPS positions:
	// for the devices without barometer, or with a defective one
	AltitudeCorrectionReplace = "replace"
	// AltitudeCorrectionCalibrate keeps the profile of the barometer, shifted to the elevation of the ground
	AltitudeCorrectionCalibrate = "calibrate"
)

// ValidAltitudeCorrection tells whether correction is one of the altitude corrections
func ValidAltitudeCorrection(correction string) bool {
	switch correction {
	case AltitudeCorrectionNone, AltitudeCorrectionReplace, AltitudeCorrectionCalibrate:
		return true
	}
	return false
}

// Devices is a device that recorded activities of a user, created at the import of its first activity.
// A unit is identified by its brand, its model and, when its files give it, its serial number: two units
// of the same model are two devices, each with its own settings (one may have a defective barometer).
// The units whose files give no serial number share one device per model, Serial empty.
type Devices struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	User      Users     `json:"-"`
	UserID    uint      `gorm:"not null;uniqueIndex:idx_devices_unit" json:"user_id"`
	// never NULL: NULL being distinct from NULL, the unique index would let duplicates in
	Brand  string `gorm:"not null;default:'';uniqueIndex:idx_devices_unit" json:"brand"`
	Model  string `gorm:"not null;default:'';uniqueIndex:idx_devices_unit" json:"model"`
	Serial string `gorm:"not null;default:'';uniqueIndex:idx_devices_unit" json:"serial_number"`
	// "Garmin Edge 530", the device_name of its activities
	Name string `json:"name"`
	// the name the user gave it, shown in place of Name; empty for Name
	Label              string `gorm:"not null;default:''" json:"label"`
	AltitudeCorrection string `gorm:"not null;default:'none'" json:"altitude_correction"`
	// the device this one was merged into: the same unit, that its files named differently (a firmware
	// update giving a serial number...). It stays as an alias: the activities of its name go to the other.
	MergedIntoID *uuid.UUID `gorm:"type:uuid;index" json:"-"`
}

// unitOf selects the devices of a unit of a user
const unitOf = "user_id = ? AND brand = ? AND model = ? AND serial = ?"

// ErrMergeDevice is a merge of a device into itself
var ErrMergeDevice = errors.New("a device can't be merged into itself")

// FindOrCreateDevice returns the id of the device of the user that recorded an activity, creating it with
// correction at its first activity, the device it was merged into when it was; nil when the file doesn't
// name the device
func (db *DB) FindOrCreateDevice(userID uint, device types.Device, name, correction string) (*uuid.UUID, error) {
	if name == "" {
		return nil, nil
	}
	if !ValidAltitudeCorrection(correction) {
		correction = AltitudeCorrectionNone
	}
	created := Devices{UserID: userID, Brand: device.Brand, Model: device.Model, Serial: device.Serial,
		Name: name, AltitudeCorrection: correction}
	// two imports at once can create the same device: the second one finds the first one's
	if err := db.Omit("User").Clauses(clause.OnConflict{DoNothing: true}).Create(&created).Error; err != nil {
		return nil, err
	}
	var found Devices
	// shared lock: a merge of the device being saved waits for this import, or this import for the merge,
	// and then reads the device it was merged into
	err := db.Select("id", "merged_into_id").Clauses(clause.Locking{Strength: "SHARE"}).
		Where(unitOf, userID, device.Brand, device.Model, device.Serial).First(&found).Error
	if err != nil {
		return nil, err
	}
	if found.MergedIntoID != nil {
		return found.MergedIntoID, nil
	}
	return &found.ID, nil
}

// DeviceAltitudeCorrection returns the altitude correction of the device of a user that recorded an
// activity (of the device it was merged into), AltitudeCorrectionNone for a device without activity yet:
// a new device starts without
func (db *DB) DeviceAltitudeCorrection(userID uint, device types.Device) (string, error) {
	var corrections []string
	err := db.Table("devices d").Joins("JOIN devices t ON t.id = COALESCE(d.merged_into_id, d.id)").
		Where("d.user_id = ? AND d.brand = ? AND d.model = ? AND d.serial = ?", userID, device.Brand, device.Model, device.Serial).
		Limit(1).Pluck("t.altitude_correction", &corrections).Error
	if err != nil || len(corrections) == 0 {
		return AltitudeCorrectionNone, err
	}
	return corrections[0], nil
}

// BackfillDevices creates the devices of the activities imported before devices existed, from the device
// their FIT file named, and links the activities to them. It returns the number of activities linked.
func (db *DB) BackfillDevices() (int64, error) {
	var linked int64
	err := db.Transaction(func(tx *gorm.DB) error {
		// the device column holds the JSON of a types.Device, in a bytea column
		const unit = `(SELECT user_id, coalesce(d->>'brand', '') AS brand, coalesce(d->>'model', '') AS model,
				coalesce(d->>'serial_number', '') AS serial, device_name, id
			FROM (SELECT user_id, device_name, id, convert_from(device, 'UTF8')::jsonb AS d
				FROM activities WHERE device_id IS NULL AND device_name <> '' AND device IS NOT NULL) a)`
		// the name of a unit is the one of its latest activity: the names of the older engines may differ
		err := tx.Exec(`INSERT INTO devices (user_id, brand, model, serial, name, altitude_correction, created_at, updated_at)
			SELECT DISTINCT ON (u.user_id, u.brand, u.model, u.serial) u.user_id, u.brand, u.model, u.serial,
				u.device_name, ?, now(), now()
			FROM `+unit+` u JOIN activities USING (id)
			ORDER BY u.user_id, u.brand, u.model, u.serial, activities.date DESC
			ON CONFLICT DO NOTHING`, AltitudeCorrectionNone).Error
		if err != nil {
			return err
		}
		result := tx.Exec(`UPDATE activities SET device_id = COALESCE(devices.merged_into_id, devices.id)
			FROM ` + unit + ` u JOIN devices USING (user_id, brand, model, serial)
			WHERE activities.id = u.id`)
		linked = result.RowsAffected
		return result.Error
	})
	return linked, err
}

// DeviceAlias is how the files of a device also named it, before it was merged
type DeviceAlias struct {
	Name   string `json:"name"`
	Brand  string `json:"brand"`
	Model  string `json:"model"`
	Serial string `json:"serial_number"`
}

// DeviceStats is a device with the activities it recorded
type DeviceStats struct {
	Devices
	Activities int64      `json:"activities"`
	LastUsed   *time.Time `json:"last_used"`
	// the firmware version of its latest activity, empty when the file doesn't tell
	Software string `json:"software"`
	// the devices merged into it
	Aliases []DeviceAlias `json:"aliases" gorm:"-"`
	// the latest recompute of its activities since the server started, nil without
	Recompute *types.RecomputeStatus `json:"recompute" gorm:"-"`
}

// GetDevicesStats returns the devices of a user, the latest used first; not the ones merged into another
func (db *DB) GetDevicesStats(userID uint) ([]DeviceStats, error) {
	return db.devicesStats("d.user_id = ?", userID)
}

// GetDeviceStats returns a device of a user, gorm.ErrRecordNotFound when the user has no such device or
// when it was merged into another
func (db *DB) GetDeviceStats(userID uint, id uuid.UUID) (DeviceStats, error) {
	stats, err := db.devicesStats("d.user_id = ? AND d.id = ?", userID, id)
	if err != nil {
		return DeviceStats{}, err
	}
	if len(stats) == 0 {
		return DeviceStats{}, gorm.ErrRecordNotFound
	}
	return stats[0], nil
}

func (db *DB) devicesStats(where string, args ...interface{}) ([]DeviceStats, error) {
	stats := []DeviceStats{}
	// the device column holds the JSON of a types.Device, in a bytea column
	err := db.Raw(`SELECT d.*, COUNT(a.id) AS activities, MAX(a.date) AS last_used,
			COALESCE((SELECT convert_from(l.device, 'UTF8')::jsonb->>'software' FROM activities l
				WHERE l.device_id = d.id ORDER BY l.date DESC LIMIT 1), '') AS software
		FROM devices d LEFT JOIN activities a ON a.device_id = d.id
		WHERE d.merged_into_id IS NULL AND `+where+`
		GROUP BY d.id ORDER BY last_used DESC NULLS LAST, `+deviceDisplayName("d")+`, d.serial`, args...).
		Scan(&stats).Error
	if err != nil || len(stats) == 0 {
		return stats, err
	}
	ids := make([]uuid.UUID, len(stats))
	index := map[uuid.UUID]int{}
	for i := range stats {
		ids[i] = stats[i].ID
		index[stats[i].ID] = i
		stats[i].Aliases = []DeviceAlias{}
	}
	var aliases []Devices
	err = db.Where("merged_into_id IN ?", ids).Order("name").Order("serial").Find(&aliases).Error
	for _, alias := range aliases {
		i := index[*alias.MergedIntoID]
		stats[i].Aliases = append(stats[i].Aliases,
			DeviceAlias{Name: alias.Name, Brand: alias.Brand, Model: alias.Model, Serial: alias.Serial})
	}
	return stats, err
}

// SetAltitudeCorrection changes the altitude correction of a device of a user, gorm.ErrRecordNotFound
// when the user has no such device
func (db *DB) SetAltitudeCorrection(userID uint, id uuid.UUID, correction string) error {
	return db.UpdateDevice(userID, id, nil, &correction)
}

// MaxDeviceLabel is the longest name a user may give a device, in characters
const MaxDeviceLabel = 64

// DisplayName is how the device is shown: the name the user gave it, else the one its files give
func (d Devices) DisplayName() string {
	if d.Label != "" {
		return d.Label
	}
	return d.Name
}

// deviceDisplayName is DisplayName in SQL, for the devices table aliased as alias
func deviceDisplayName(alias string) string {
	return "COALESCE(NULLIF(" + alias + ".label, ''), " + alias + ".name)"
}

// UpdateDevice changes the name the user gave a device of theirs ("" to show the name its files give) and
// its altitude correction, each when not nil; gorm.ErrRecordNotFound when the user has no such device
func (db *DB) UpdateDevice(userID uint, id uuid.UUID, label, correction *string) error {
	updates := map[string]interface{}{}
	if label != nil {
		updates["label"] = *label
	}
	if correction != nil {
		updates["altitude_correction"] = *correction
	}
	if len(updates) == 0 {
		return nil
	}
	result := db.Model(&Devices{}).Where("user_id = ? AND id = ? AND merged_into_id IS NULL", userID, id).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// DeviceLabel returns the name the user gave a device, "" when they gave none or there is no such device
func (db *DB) DeviceLabel(id uuid.UUID) (string, error) {
	var labels []string
	err := db.Model(&Devices{}).Where("id = ?", id).Limit(1).Pluck("label", &labels).Error
	if err != nil || len(labels) == 0 {
		return "", err
	}
	return labels[0], nil
}

// MergeDevices merges a device of a user into another: the same unit, that its files named differently.
// Its activities go to the other one, and so do the activities imported later under its name; the devices
// merged into it are merged into the other one too. The settings are the ones of the other device: the
// activities moved keep the altitudes their analysis gave, until they are recomputed.
// gorm.ErrRecordNotFound when one of them isn't a device of the user (or was merged already),
// ErrMergeDevice when they are the same.
func (db *DB) MergeDevices(userID uint, id, intoID uuid.UUID) error {
	if id == intoID {
		return ErrMergeDevice
	}
	return db.Transaction(func(tx *gorm.DB) error {
		// locked: an import of the merged device waits for the merge, then reads the device it was merged into
		var found []Devices
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND id IN ? AND merged_into_id IS NULL", userID, []uuid.UUID{id, intoID}).
			Order("id"). // in the same order by every merge: two merges of the same devices can't deadlock
			Find(&found).Error
		if err != nil {
			return err
		}
		if len(found) != 2 {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Model(&Activity{}).Where("device_id = ?", id).Update("device_id", intoID).Error; err != nil {
			return err
		}
		return tx.Model(&Devices{}).Where("id = ? OR merged_into_id = ?", id, id).
			Update("merged_into_id", intoID).Error
	})
}
