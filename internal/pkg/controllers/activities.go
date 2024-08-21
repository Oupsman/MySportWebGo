package controllers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/services/activityService"
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
	App := c.MustGet("App")
	cookie, err := c.Cookie("mysportweb_session")
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
	db := App.(*app.App).DB
	userUUID, err := utils.GetUserUUID(cookie)
	user, err := db.GetUserByUUID(userUUID)

	equipment := db.GetDefaultEquipment(user.ID)

	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
	// single file
	file, _ := c.FormFile("file")

	// Create the dst directory
	baseDir := "MEDIA/" + userUUID.String() + "/Activities/"
	if err := os.MkdirAll(filepath.Dir(baseDir), 0770); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}

	dstFile := baseDir + file.Filename

	// Upload the file to specific dstFile.
	err = c.SaveUploadedFile(file, dstFile)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
	activity, err := activityService.SumAnalyze(dstFile+"/"+file.Filename, user, equipment)
	err = db.CreateActivity(activity)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
	c.JSON(http.StatusOK, gin.H{"id": activity.ID.String()})
	// 	c.String(http.StatusOK, fmt.Sprintf("'%s' uploaded!", file.Filename))
}

func ListActivities(c *gin.Context) {
	App := c.MustGet("App")
	cookie, err := c.Cookie("mysportweb_session")
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
	db := App.(*app.App).DB
	userUUID, err := utils.GetUserUUID(cookie)
	user, err := db.GetUserByUUID(userUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
	activities, err := db.GetActivitiesByUser(user.ID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}

	c.JSON(http.StatusOK, gin.H{"activities": activities})
}

func UpdateActivity(c *gin.Context) {
	App := c.MustGet("App")
	cookie, err := c.Cookie("mysportweb_session")
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
	db := App.(*app.App).DB
	userUUID, err := utils.GetUserUUID(cookie)
	user, err := db.GetUserByUUID(userUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}

	activityUUID, err := uuid.FromBytes([]byte(c.Param("id")))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}

	activity, err := db.GetActivity(activityUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
	if activity.User.ID != user.ID {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "You are not the owner of this activity"})
	}
	err = c.BindJSON(&activity)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
	err = db.UpdateActivity(activity)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
	c.JSON(http.StatusOK, gin.H{"id": activity.ID.String()})
}

func GetActivity(c *gin.Context) {
	App := c.MustGet("App")
	cookie, err := c.Cookie("mysportweb_session")
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
	db := App.(*app.App).DB
	userUUID, err := utils.GetUserUUID(cookie)
	user, err := db.GetUserByUUID(userUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}

	activityUUID, err := uuid.FromBytes([]byte(c.Param("id")))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}

	activity, err := db.GetActivity(activityUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
	if (activity.Visibility == 0 && activity.User.ID != user.ID) || activity.Visibility == 2 {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "You can't access this activity"})
	}
	c.JSON(http.StatusOK, gin.H{"activity": activity})
}

func DeleteActivity(c *gin.Context) {
	App := c.MustGet("App")
	cookie, err := c.Cookie("mysportweb_session")
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
	db := App.(*app.App).DB
	userUUID, err := utils.GetUserUUID(cookie)
	user, err := db.GetUserByUUID(userUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}

	activityUUID, err := uuid.FromBytes([]byte(c.Param("id")))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}

	activity, err := db.GetActivity(activityUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
	if activity.User.ID != user.ID {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "You are not the owner of this activity"})
	}
	err = db.DeleteActivity(activityUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
	c.JSON(http.StatusOK, gin.H{"id": activity.ID.String()})
}
