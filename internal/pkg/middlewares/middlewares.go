package middlewares

import (
	"MySportWeb/internal/pkg/utils"
	"net/http"

	"github.com/gin-gonic/gin"
)

func IsAuthorized() gin.HandlerFunc {

	return func(c *gin.Context) {
		_, err := utils.ParseBearerToken(c.GetHeader("Authorization"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"message": "unauthorized: " + err.Error(),
			})
			return
		}

		c.Next()
	}
}

func IsAdmin() gin.HandlerFunc {

	return func(c *gin.Context) {
		claims, err := utils.ParseBearerToken(c.GetHeader("Authorization"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"message": "unauthorized: " + err.Error(),
			})
			return
		}

		if claims["role"] != "Admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"message": "forbidden",
			})
			return
		}

		c.Next()
	}
}
