package controllers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/services/activity"
	"github.com/gin-gonic/gin"
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
	_, err = activity.SumAnalyze(dstFile + "/" + file.Filename)
	// 	c.String(http.StatusOK, fmt.Sprintf("'%s' uploaded!", file.Filename))
}
