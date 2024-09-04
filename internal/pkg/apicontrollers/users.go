package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"time"
)

// /MEDIA/user uuid/profile pictures/profile picture.png

func Login(c *gin.Context) {

	var user models.Users

	App := c.MustGet("App")
	db := App.(*app.App).DB

	if err := c.ShouldBindJSON(&user); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	var existingUser models.Users

	db.Where("Username = ?", user.Username).First(&existingUser)

	if existingUser.ID == 0 {
		c.JSON(400, gin.H{"error": "user does not exist"})
		return
	}

	errHash := utils.CompareHashPassword(user.Password, existingUser.Password)

	if !errHash {
		c.JSON(400, gin.H{"error": "invalid password"})
		return
	}

	expirationTime := time.Now().Add(720 * time.Hour)

	claims := jwt.MapClaims{
		"authorized": true,
		"role":       existingUser.Role,
		"exp":        expirationTime.Unix(),
		"iss":        "mysportweb",
		"sub":        existingUser.ID,
		"uuid":       existingUser.UUID.String(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(vars.SecretKey))

	if err != nil {
		c.JSON(500, gin.H{"error": "could not generate token"})
		return
	}
	c.SetCookie("mysportweb_session", tokenString, 3600, "/", "localhost", false, true)
	c.JSON(200, gin.H{"token": tokenString})
}

func Register(c *gin.Context) {

	var user models.Users
	App := c.MustGet("App")
	db := App.(*app.App).DB

	if err := c.ShouldBindJSON(&user); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	if user.Username == "" || user.Password == "" || user.Email == "" {
		c.JSON(400, gin.H{"error": "missing required fields"})
		return
	}

	var existingUser models.Users

	db.Where("Username = ?", user.Username).First(&existingUser)

	if existingUser.ID != 0 {
		c.JSON(409, gin.H{"error": "user already exists"})
		return
	}

	var errHash error
	user.Password, errHash = utils.GenerateHashPassword(user.Password)

	if errHash != nil {
		c.JSON(500, gin.H{"error": "could not generate password hash"})
		return
	}

	db.Create(&user)

	if user.ID == 1 {
		user.Role = "Admin"
		db.Save(&user)
	} else {
		user.Role = "User"
		db.Save(&user)
	}

	c.JSON(201, gin.H{"success": "user registered"})
}

func Logout(c *gin.Context) {

	c.JSON(200, gin.H{"success": "user logged out"})
}

func GetUser(c *gin.Context) {
	App := c.MustGet("App")
	db := App.(*app.App).DB
	bearerToken := c.Request.Header.Get("Authorization")
	userUUID, err := utils.GetUserUUID(bearerToken)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	user, err := db.GetUserByUUID(userUUID)

	if err != nil {
		c.JSON(400, gin.H{"error": "user not found"})
	}
	c.JSON(200, user)
}

func UpdateUser(c *gin.Context) {

	var updatedUser models.Users
	var user types.UserBody
	App := c.MustGet("App")
	db := App.(*app.App).DB
	err := c.ShouldBindJSON(&user)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	bearerToken := c.Request.Header.Get("Authorization")
	UserID, err := utils.GetUserID(bearerToken)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	updatedUser.ID = uint(UserID)
	currentUser, err := db.GetUser(UserID)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	if !utils.CompareHashPassword(user.OldPassword, currentUser.Password) {
		c.JSON(401, gin.H{
			"error": "wrong password",
		})
		return
	}

	updatedUser.ID = uint(UserID)
	updatedUser.Email = user.Email
	updatedUser.Username = currentUser.Username
	updatedUser.Role = currentUser.Role
	if user.Password != "" {
		newHash, err := utils.GenerateHashPassword(user.Password)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		updatedUser.Password = newHash
	} else {
		updatedUser.Password = currentUser.Password
	}

	err = db.UpdateUser(updatedUser)

	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "user updated successfully"})
}
