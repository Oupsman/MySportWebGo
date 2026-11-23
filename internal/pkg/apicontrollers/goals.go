package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/services/activityService"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	maxGoalName   = 100
	maxGoalSports = 20
	// a target above it is a mistake: 1e9 m, s or activities
	maxGoalTarget = 1e9
	// the longest custom period, in days
	maxGoalDays = 3660
)

// goalInput is what the user sets of a goal: target in meters (distance, elevation), seconds (duration) or
// a count (activities); starts_on and ends_on (YYYY-MM-DD, included) for a custom period only
type goalInput struct {
	Name     string   `json:"name"`
	Metric   string   `json:"metric"`
	Target   float64  `json:"target"`
	Sports   []string `json:"sports"`
	Period   string   `json:"period"`
	StartsOn string   `json:"starts_on"`
	EndsOn   string   `json:"ends_on"`
}

// apply checks the input and copies it to goal
func (in goalInput) apply(goal *models.Goal) error {
	name := strings.TrimSpace(in.Name)
	switch {
	case len([]rune(name)) > maxGoalName:
		return fmt.Errorf("the name of a goal is %d characters at most", maxGoalName)
	case !slices.Contains(models.GoalMetrics, in.Metric):
		return errors.New("metric must be distance, duration, elevation or activities")
	case !slices.Contains(models.GoalPeriods, in.Period):
		return errors.New("period must be week, month, year or custom")
	case !(in.Target > 0) || in.Target > maxGoalTarget:
		return errors.New("the target must be above 0")
	case len(in.Sports) > maxGoalSports:
		return fmt.Errorf("a goal counts %d sports at most", maxGoalSports)
	}
	sports := models.SportList{}
	for _, sport := range in.Sports {
		if !activityService.IsSportName(sport) {
			return errors.New("unknown sport " + strconv.Quote(sport))
		}
		if !slices.Contains(sports, sport) {
			sports = append(sports, sport)
		}
	}
	goal.StartsOn, goal.EndsOn = nil, nil
	if in.Period == "custom" {
		starts, err := time.Parse(time.DateOnly, in.StartsOn)
		if err != nil {
			return errors.New("a custom period needs starts_on, YYYY-MM-DD")
		}
		ends, err := time.Parse(time.DateOnly, in.EndsOn)
		if err != nil {
			return errors.New("a custom period needs ends_on, YYYY-MM-DD")
		}
		if ends.Before(starts) {
			return errors.New("the period ends before it starts")
		}
		if ends.Sub(starts) > maxGoalDays*24*time.Hour {
			return errors.New("a custom period is 10 years at most")
		}
		goal.StartsOn, goal.EndsOn = &starts, &ends
	}
	goal.Name = name
	goal.Metric = in.Metric
	goal.Target = in.Target
	if in.Metric == "activities" {
		goal.Target = float64(int(in.Target + 0.5))
		if goal.Target < 1 {
			return errors.New("the target must be 1 activity at least")
		}
	}
	goal.Sports = sports
	goal.Period = in.Period
	return nil
}

// userGoal returns the goal of the :id parameter of the user of the request; it aborts the request on error
func userGoal(c *gin.Context) (models.Goal, bool) {
	db := c.MustGet("App").(*app.App).DB
	id, err := utils.GetUUID(c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return models.Goal{}, false
	}
	goal, err := db.GetGoal(middlewares.CurrentUser(c).ID, id)
	if err != nil {
		lookupError(c, err, "goal")
		return goal, false
	}
	return goal, true
}

// answerGoal answers a goal with its progress
func answerGoal(c *gin.Context, status int, goal models.Goal) {
	db := c.MustGet("App").(*app.App).DB
	_, location := currentUserLocation(c)
	s, err := db.GoalStatusAt(goal, time.Now().In(location))
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(status, s)
}

// GetGoals answers the goals of the user with their progress, in their timezone
func GetGoals(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user, location := currentUserLocation(c)
	goals, err := db.GetGoals(user.ID, time.Now().In(location))
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, goals)
}

// saveGoal checks the input of the request, saves it to goal and answers it
func saveGoal(c *gin.Context, goal models.Goal, status int) {
	db := c.MustGet("App").(*app.App).DB
	_, location := currentUserLocation(c)
	var input goalInput
	if err := c.ShouldBindJSON(&input); err != nil {
		badRequest(c, err)
		return
	}
	if err := input.apply(&goal); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := db.SaveGoal(&goal, time.Now().In(location)); err != nil {
		internalError(c, err)
		return
	}
	answerGoal(c, status, goal)
}

func CreateGoal(c *gin.Context) {
	saveGoal(c, models.Goal{UserID: middlewares.CurrentUser(c).ID}, http.StatusCreated)
}

func UpdateGoal(c *gin.Context) {
	goal, ok := userGoal(c)
	if !ok {
		return
	}
	saveGoal(c, goal, http.StatusOK)
}

func DeleteGoal(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	goal, ok := userGoal(c)
	if !ok {
		return
	}
	if err := db.DeleteGoal(goal.UserID, goal.ID); err != nil {
		lookupError(c, err, "goal")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": "goal deleted"})
}
