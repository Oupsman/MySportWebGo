package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/services/activityService"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
	"os"
	"path/filepath"
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

	fmt.Println(uploadParams)
	file := uploadParams.File
	item := uploadParams.Item
	// count := uploadParams.Count
	// Create the dst directory
	baseDir := "MEDIA/" + userUUID.String() + "/Activities/"
	if err := os.MkdirAll(filepath.Dir(baseDir), 0770); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dstFile := baseDir + file.Filename
	fmt.Println("DstFile : ", dstFile)
	// Upload the file to specific dstFile.
	err = c.SaveUploadedFile(file, dstFile)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	activity, err := activityService.SumAnalyze(dstFile, user, equipment)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	activity.Filename = file.Filename
	activity.FilePath = dstFile

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

	err = db.CreateActivity(&activity)
	if err != nil {
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
	activities, err := db.GetActivitiesByUser(user.ID)
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
	activity.Title = newActivity.Title
	activity.CanComments = newActivity.CanComments
	activity.IsCommute = newActivity.IsCommute
	activity.EquipmentID = newActivity.EquipmentID
	activity.Visibility = newActivity.Visibility

	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
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
	err = db.DeleteActivity(activityUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": activity.ID.String()})
}
