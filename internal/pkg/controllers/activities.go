package controllers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"github.com/gin-gonic/gin"
	"github.com/muktihari/fit/decoder"
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
	_, err = SumAnalyze(dstFile + "/" + file.Filename)
	// 	c.String(http.StatusOK, fmt.Sprintf("'%s' uploaded!", file.Filename))
}

func SumAnalyze(filePath string) (models.Activity, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return models.Activity{}, err
	}
	defer f.Close()

	dec := decoder.NewRaw()

	return models.Activity{}, nil
}
