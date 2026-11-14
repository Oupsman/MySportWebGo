package controllers

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

// DEAD CODE: not used anywhere, commented out to keep it out of the build (kept for reference).
// func SignUp(c *gin.Context) {
// 	c.HTML(http.StatusOK, "signup.tmpl", gin.H{
// 		"title": "MySportWeb Signup",
// 	})
// }

// DEAD CODE: not used anywhere, commented out to keep it out of the build (kept for reference).
// func Login(c *gin.Context) {
// 	c.HTML(http.StatusOK, "login.tmpl", gin.H{
// 		"title": "MySportWeb Login",
// 	})
// }

func IsAuthenticated(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message": "pong",
	})
}
