package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/services/activityService"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

// Directory structures :
// /MEDIA/user uuid/Activities/raw fit file
// /MEDIA/user uuid/thumnails/activity uuid.png
//

func UploadActivity(c *gin.Context) {
	var uploadParams types.ActivityUpload

	App := c.MustGet("App")
	bearerToken := c.Request.Header.Get("Authorization")
	userUUID, err := utils.GetUserUUID(bearerToken)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	db := App.(*app.App).DB

	user, err := db.GetUserByUUID(userUUID)

	equipment := db.GetDefaultEquipment(user.ID)

	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// single file
	// file, _ := c.FormFile("file")

	err = c.ShouldBind(&uploadParams)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	file := uploadParams.File
	item := uploadParams.Item
	// count := uploadParams.Count
	baseDir := filepath.Join(activityService.MediaDir, userUUID.String(), "Activities")
	if err := os.MkdirAll(baseDir, 0770); err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// analysed from a temporary file: a refused upload must not overwrite the FIT file of an activity
	// with the same file name, nor stay on disk
	tmp, err := os.CreateTemp(baseDir, "upload-*.fit")
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath) // fails harmlessly once the file is moved in place
	if err = c.SaveUploadedFile(file, tmpPath); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	activity, err := activityService.SumAnalyze(tmpPath, user, equipment)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	activity.ID = uuid.New()
	activity.Filename = filepath.Base(file.Filename)

	// checked again when saving; checked here to answer before generating the thumbnail, which is slow
	existing, err := db.FindDuplicateActivity(user.ID, activity.Sport, activity.Date)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if existing != nil {
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "this activity was already imported", "id": existing.String()})
		return
	}

	// reset LastImport status
	if item == 0 {
		err = db.ResetImportStatus(user.ID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

	}

	err = activityService.GenerateThumbnail(activity)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// the FIT file keeps its name, prefixed by the activity id when another file has it
	store := func(activity *models.Activity) error {
		activity.FilePath = filepath.Join(baseDir, activity.Filename)
		if _, err := os.Stat(activity.FilePath); err == nil {
			activity.FilePath = filepath.Join(baseDir, activity.ID.String()+"_"+activity.Filename)
		}
		return os.Rename(tmpPath, activity.FilePath)
	}
	err = activityService.CreateActivity(&db, &activity, store)
	if err != nil {
		os.Remove(activityService.ThumbnailPath(userUUID, activity.ID))
		var duplicate *activityService.DuplicateError
		if errors.As(err, &duplicate) {
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": err.Error(), "id": duplicate.ExistingID.String()})
			return
		}
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": activity.ID.String()})
}

func ListActivities(c *gin.Context) {
	App := c.MustGet("App")
	bearerToken := c.Request.Header.Get("Authorization")
	userUUID, err := utils.GetUserUUID(bearerToken)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	db := App.(*app.App).DB
	user, err := db.GetUserByUUID(userUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	start, err := strconv.Atoi((c.Param("start")))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	count, err := strconv.Atoi(c.Param("count"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	activities, err := db.GetActivitiesByUser(user.ID, start, count)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"activities": activities})
}

func UpdateActivity(c *gin.Context) {
	var newActivity models.Activity

	App := c.MustGet("App")
	bearerToken := c.Request.Header.Get("Authorization")
	userUUID, err := utils.GetUserUUID(bearerToken)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	db := App.(*app.App).DB
	user, err := db.GetUserByUUID(userUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	activityUUID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	activity, err := db.GetActivity(activityUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if activity.User.ID != user.ID {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "You are not the owner of this activity"})
		return
	}
	err = c.ShouldBindJSON(&newActivity)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if newActivity.EquipmentID != nil {
		equipment, err := db.GetEquipment(*newActivity.EquipmentID)
		if err != nil || equipment.UserID != int(user.ID) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "unknown equipment"})
			return
		}
	}
	if newActivity.Visibility < 0 || newActivity.Visibility > 2 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid visibility"})
		return
	}
	activity.Title = newActivity.Title
	activity.CanComments = newActivity.CanComments
	activity.IsCommute = newActivity.IsCommute
	activity.EquipmentID = newActivity.EquipmentID
	// the preloaded association would override EquipmentID when saving
	activity.Equipment = models.Equipments{}
	activity.Visibility = newActivity.Visibility
	err = db.UpdateActivity(activity)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": activity.ID.String()})
}

func GetActivity(c *gin.Context) {
	var userID uint
	App := c.MustGet("App")
	db := App.(*app.App).DB

	activityUUID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	activity, err := db.GetActivity(activityUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	bearerToken := c.Request.Header.Get("Authorization")
	fmt.Println("Token: ", bearerToken)
	if bearerToken != "" {
		userUUID, err := utils.GetUserUUID(bearerToken)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		user, err := db.GetUserByUUID(userUUID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		userID = user.ID
		if activity.Visibility == 0 && activity.User.ID != user.ID {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "You can't access this activity"})
			return
		}
	} else if activity.Visibility != 2 {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "You can't access this activity"})
		return
	}
	if activity.User.ID != userID {
		// Deleting non-public datas if user is not the owner
		activity.GpsPoints = nil
		activity.GpsTimeStamps = nil
		activity.StartPosition = nil
		activity.EndPosition = nil
		activity.User = models.Users{}
		activity.Equipment = models.Equipments{}
		// TODO : is there more fields that needs to be nilled ?

	}
	c.JSON(http.StatusOK, gin.H{"activity": activity})
}

func DeleteActivity(c *gin.Context) {
	App := c.MustGet("App")
	bearerToken := c.Request.Header.Get("Authorization")
	userUUID, err := utils.GetUserUUID(bearerToken)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	db := App.(*app.App).DB
	user, err := db.GetUserByUUID(userUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	activityUUID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	activity, err := db.GetActivity(activityUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if activity.User.ID != user.ID {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "You are not the owner of this activity"})
		return
	}
	err = activityService.DeleteActivity(&db, activity, App.(*app.App).Logger)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": activity.ID.String()})
}

func GetSimilarActivities(c *gin.Context) {
	App := c.MustGet("App")
	bearerToken := c.Request.Header.Get("Authorization")
	userUUID, err := utils.GetUserUUID(bearerToken)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	db := App.(*app.App).DB
	user, err := db.GetUserByUUID(userUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	activityUUID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	activity, err := db.GetActivity(activityUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if activity.User.ID != user.ID {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "You are not the owner of this activity"})
		return
	}

	similar := []types.RouteActivity{}
	if activity.RouteID != nil {
		similar, err = db.GetActivitiesByRoute(*activity.RouteID, activity.ID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"activities": similar})
}
