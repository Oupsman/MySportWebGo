package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/utils"
	"encoding/csv"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func ImportHealthDatas(c *gin.Context) {
	var uploadParams types.HealthDatasUpload
	App := c.MustGet("App").(*app.App)
	db := App.DB
	bearerToken := c.Request.Header.Get("Authorization")
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
	// Iterate over the CSV file received
	err = c.ShouldBind(&uploadParams)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	file := uploadParams.File
	baseDir := "tmp/"
	if err := os.MkdirAll(filepath.Dir(baseDir), 0770); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dstFile := baseDir + file.Filename

	csvFile, err := os.Open(dstFile)
	if err != nil {
		panic(err)
	}
	defer csvFile.Close()

	csvReader := csv.NewReader(csvFile)
	for {
		var healthData models.HealthData
		healthData.User = user
		row, err := csvReader.Read()
		if err == io.EOF {
			break
		} else if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		healthData.Date, err = time.Parse("2006-01-02 15:04", row[0])
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		healthData.Weight, err = strconv.ParseFloat(row[1], 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		healthData.Fat, err = strconv.ParseFloat(row[2], 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		healthData.Bone, err = strconv.ParseFloat(row[3], 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		healthData.Muscle, err = strconv.ParseFloat(row[4], 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		healthData.BodyWater, err = strconv.ParseFloat(row[5], 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		err = db.CreateHealthData(healthData)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

	}

	c.JSON(http.StatusCreated, gin.H{"message": "file successfully imported"})
}

func GetHealthDatas(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	db := App.DB
	bearerToken := c.Request.Header.Get("Authorization")
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
	healthDatas, err := db.GetHealthDatas(user.ID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, healthDatas)
}
