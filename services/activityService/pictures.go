package activityService

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// equipmentDir is the directory of the pictures of an equipment
func equipmentDir(userUUID, equipmentID uuid.UUID) string {
	return filepath.Join(MediaDir, userUUID.String(), "equipments", equipmentID.String())
}

// SaveEquipmentPicture writes a processed picture of an equipment and returns the URL path it is served at.
// The file name is new on each upload: browsers don't show the replaced picture from their cache.
func SaveEquipmentPicture(userUUID, equipmentID uuid.UUID, picture []byte) (string, error) {
	dir := equipmentDir(userUUID, equipmentID)
	if err := os.MkdirAll(dir, 0770); err != nil {
		return "", err
	}
	path := filepath.Join(dir, uuid.NewString()+".jpg")
	if err := os.WriteFile(path, picture, 0660); err != nil {
		return "", err
	}
	return "/" + filepath.ToSlash(path), nil
}

// RemoveEquipmentPicture removes a picture saved by SaveEquipmentPicture, from its URL path
func RemoveEquipmentPicture(userUUID uuid.UUID, urlPath string) error {
	if urlPath == "" {
		return nil
	}
	return removeUserFile(userUUID, filepath.FromSlash(strings.TrimPrefix(urlPath, "/")))
}

// RemoveEquipmentPictures removes the pictures of a deleted equipment
func RemoveEquipmentPictures(userUUID, equipmentID uuid.UUID) error {
	if userUUID == uuid.Nil || equipmentID == uuid.Nil {
		return errors.New("refusing to remove the pictures without user or equipment")
	}
	return os.RemoveAll(equipmentDir(userUUID, equipmentID))
}
