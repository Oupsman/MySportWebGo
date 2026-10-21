package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/utils"
	"github.com/gin-gonic/gin"
	"net/http"
)

func ImportHealthDatas(c *gin.Context) {
	// TODO: CSV column mapping (Date/Weight/fatpc/musclepc/resthr/vo2max) is not
	// defined yet, so row parsing and persistence are not implemented.
	c.JSON(http.StatusNotImplemented, gin.H{"error": "health data import is not implemented yet"})
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
