package middlewares

import (
	"MySportWeb/internal/pkg/utils"
	jwt "github.com/dgrijalva/jwt-go"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

func IsAuthorized() gin.HandlerFunc {

	return func(c *gin.Context) {

		bearerToken := c.GetHeader("Authorization")
		reqToken := strings.Split(bearerToken, " ")[1]
		_, err := utils.ParseToken(reqToken)

		if err != nil {
			if err == jwt.ErrSignatureInvalid {
				c.JSON(http.StatusUnauthorized, gin.H{
					"message": "mdw nauthorized",
				})
				c.Abort()
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{
				"message": "mdw bad request: " + err.Error(),
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

func IsAdmin() gin.HandlerFunc {

	return func(c *gin.Context) {

		bearerToken := c.GetHeader("Authorization")
		reqToken := strings.Split(bearerToken, " ")[1]
		claims, err := utils.ParseToken(reqToken)

		if err != nil {
			if err == jwt.ErrSignatureInvalid {
				c.JSON(http.StatusUnauthorized, gin.H{
					"message": "mdw nauthorized",
				})
				c.Abort()
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{
				"message": "mdw bad request",
			})
			c.Abort()
			return
		}

		if claims["role"] != "Admin" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"message": "mdw unauthorized",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
