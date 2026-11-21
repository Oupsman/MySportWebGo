package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// The validity of the invitations, in days
const (
	DefaultInvitationDays = 7
	MaxInvitationDays     = 90
)

type invitationBody struct {
	// Email, when set, is the only email the invitation registers
	Email string `json:"email"`
	// Days is how long the invitation is valid, DefaultInvitationDays when 0
	Days int `json:"days"`
}

// GetInvitations lists the invitations of the instance, for the admins
func GetInvitations(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	invitations, err := App.DB.GetInvitations()
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(200, gin.H{"invitations": invitations})
}

// CreateInvitation creates an invitation, answering its code once: only its hash is kept. The link opens
// the registration page of the web application with the code filled in.
func CreateInvitation(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	var body invitationBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	body.Email = strings.TrimSpace(body.Email)
	if body.Email != "" {
		if address, err := mail.ParseAddress(body.Email); err != nil || address.Address != body.Email {
			c.JSON(400, gin.H{"error": "invalid email"})
			return
		}
	}
	if body.Days == 0 {
		body.Days = DefaultInvitationDays
	}
	if body.Days < 1 || body.Days > MaxInvitationDays {
		c.JSON(400, gin.H{"error": "days must be between 1 and 90"})
		return
	}
	code := utils.NewCode()
	invitation := models.Invitations{
		CreatedByID: middlewares.CurrentUser(c).ID,
		Hash:        utils.HashCode(code),
		Hint:        code[:4],
		Email:       body.Email,
		ExpiresAt:   time.Now().Add(time.Duration(body.Days) * 24 * time.Hour),
	}
	if err := App.DB.CreateInvitation(&invitation); err != nil {
		internalError(c, err)
		return
	}
	c.JSON(201, gin.H{"invitation": invitation, "code": code, "link": InvitationLink(code)})
}

// InvitationLink is the sign up page of the web application (hash routes) with the code of an invitation
func InvitationLink(code string) string {
	return strings.TrimRight(vars.Domain, "/") + "/#/signup?invitation=" + url.QueryEscape(code)
}

// RevokeInvitation deletes an unused invitation
func RevokeInvitation(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid invitation id"})
		return
	}
	revoked, err := App.DB.RevokeInvitation(id)
	if err != nil {
		internalError(c, err)
		return
	}
	if !revoked {
		c.JSON(404, gin.H{"error": "no unused invitation with this id"})
		return
	}
	c.JSON(200, gin.H{"success": "invitation revoked"})
}
