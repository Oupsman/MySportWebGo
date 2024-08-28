package controllers

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

func SignUp(c *gin.Context) {
	c.HTML(http.StatusOK, "signup.tmpl", gin.H{
		"title": "MySportWeb Signup",
	})
}

func Login(c *gin.Context) {
	c.HTML(http.StatusOK, "login.tmpl", gin.H{
		"title": "MySportWeb Login",
	})
}

func IsAuthenticated(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message": "pong",
	})
}
