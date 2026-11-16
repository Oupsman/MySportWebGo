package models

import (
	"MySportWeb/internal/pkg/types"
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
	Name               string `json:"name"`
	AltitudeCorrection string `gorm:"not null;default:'none'" json:"altitude_correction"`
}

// FindOrCreateDevice returns the id of the device of the user that recorded an activity, creating it with
// correction at its first activity; nil when the file doesn't name the device
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
	err := db.Select("id").
		Where("user_id = ? AND brand = ? AND model = ? AND serial = ?", userID, device.Brand, device.Model, device.Serial).
		First(&found).Error
	if err != nil {
		return nil, err
	}
	return &found.ID, nil
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
		result := tx.Exec(`UPDATE activities SET device_id = devices.id
			FROM ` + unit + ` u JOIN devices USING (user_id, brand, model, serial)
			WHERE activities.id = u.id`)
		linked = result.RowsAffected
		return result.Error
	})
	return linked, err
}

// DeviceStats is a device with the activities it recorded
type DeviceStats struct {
	Devices
	Activities int64      `json:"activities"`
	LastUsed   *time.Time `json:"last_used"`
	// the firmware version of its latest activity, empty when the file doesn't tell
	Software string `json:"software"`
}

// GetDevicesStats returns the devices of a user, the latest used first
func (db *DB) GetDevicesStats(userID uint) ([]DeviceStats, error) {
	return db.devicesStats("d.user_id = ?", userID)
}

// GetDeviceStats returns a device of a user, gorm.ErrRecordNotFound when the user has no such device
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
		WHERE `+where+` GROUP BY d.id ORDER BY last_used DESC NULLS LAST, d.name, d.serial`, args...).
		Scan(&stats).Error
	return stats, err
}

// SetAltitudeCorrection changes the altitude correction of a device of a user, gorm.ErrRecordNotFound
// when the user has no such device
func (db *DB) SetAltitudeCorrection(userID uint, id uuid.UUID, correction string) error {
	result := db.Model(&Devices{}).Where("user_id = ? AND id = ?", userID, id).
		Update("altitude_correction", correction)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
