package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/services/activityService"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Directory structures :
// /MEDIA/user uuid/Activities/raw fit file
// /MEDIA/user uuid/thumnails/activity uuid.png
//

func UploadActivity(c *gin.Context) {
	var uploadParams types.ActivityUpload

	App := c.MustGet("App")
	bearerToken := c.Request.Header.Get("Authorization")
	userUUID, err := utils.GetUserUUID(bearerToken)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	db := App.(*app.App).DB

	user, err := db.GetUserByUUID(userUUID)

	equipment := db.GetDefaultEquipment(user.ID)

	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// single file
	// file, _ := c.FormFile("file")

	err = c.ShouldBind(&uploadParams)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	file := uploadParams.File
	item := uploadParams.Item
	// count := uploadParams.Count
	baseDir := filepath.Join(activityService.MediaDir, userUUID.String(), "Activities")
	if err := os.MkdirAll(baseDir, 0770); err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// analysed from a temporary file: a refused upload must not overwrite the FIT file of an activity
	// with the same file name, nor stay on disk
	tmp, err := os.CreateTemp(baseDir, "upload-*.fit")
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath) // fails harmlessly once the file is moved in place
	if err = c.SaveUploadedFile(file, tmpPath); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	activity, err := activityService.SumAnalyze(tmpPath, user, equipment)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	activity.ID = uuid.New()
	activity.Filename = filepath.Base(file.Filename)

	// checked again when saving; checked here to answer before generating the thumbnail, which is slow
	existing, err := db.FindDuplicateActivity(user.ID, activity.Sport, activity.Date)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if existing != nil {
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "this activity was already imported", "id": existing.String()})
		return
	}

	// reset LastImport status
	if item == 0 {
		err = db.ResetImportStatus(user.ID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

	}

	err = activityService.GenerateThumbnail(activity)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// the FIT file keeps its name, prefixed by the activity id when another file has it
	store := func(activity *models.Activity) error {
		activity.FilePath = filepath.Join(baseDir, activity.Filename)
		if _, err := os.Stat(activity.FilePath); err == nil {
			activity.FilePath = filepath.Join(baseDir, activity.ID.String()+"_"+activity.Filename)
		}
		return os.Rename(tmpPath, activity.FilePath)
	}
	err = activityService.CreateActivity(&db, &activity, store)
	if err != nil {
		os.Remove(activityService.ThumbnailPath(userUUID, activity.ID))
		var duplicate *activityService.DuplicateError
		if errors.As(err, &duplicate) {
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": err.Error(), "id": duplicate.ExistingID.String()})
			return
		}
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": activity.ID.String()})
}

func ListActivities(c *gin.Context) {
	App := c.MustGet("App")
	bearerToken := c.Request.Header.Get("Authorization")
	userUUID, err := utils.GetUserUUID(bearerToken)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	db := App.(*app.App).DB
	user, err := db.GetUserByUUID(userUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	start, err := strconv.Atoi((c.Param("start")))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	count, err := strconv.Atoi(c.Param("count"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	activities, err := db.GetActivitiesByUser(user.ID, start, count)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"activities": activities})
}

func UpdateActivity(c *gin.Context) {
	var newActivity models.Activity

	App := c.MustGet("App")
	bearerToken := c.Request.Header.Get("Authorization")
	userUUID, err := utils.GetUserUUID(bearerToken)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	db := App.(*app.App).DB
	user, err := db.GetUserByUUID(userUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	activityUUID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	activity, err := db.GetActivity(activityUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if activity.User.ID != user.ID {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "You are not the owner of this activity"})
		return
	}
	err = c.ShouldBindJSON(&newActivity)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if newActivity.EquipmentID != nil {
		equipment, err := db.GetEquipment(*newActivity.EquipmentID)
		if err != nil || equipment.UserID != int(user.ID) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "unknown equipment"})
			return
		}
	}
	if newActivity.Visibility < 0 || newActivity.Visibility > 2 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid visibility"})
		return
	}
	activity.Title = newActivity.Title
	activity.CanComments = newActivity.CanComments
	activity.IsCommute = newActivity.IsCommute
	if activity.IsCommute && activity.Co2 == 0 {
		activity.Co2 = activityService.Commute(*activity)
	}
	activity.EquipmentID = newActivity.EquipmentID
	// the preloaded association would override EquipmentID when saving
	activity.Equipment = models.Equipments{}
	activity.Visibility = newActivity.Visibility
	err = db.UpdateActivity(activity)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": activity.ID.String()})
}

func GetActivity(c *gin.Context) {
	var userID uint
	App := c.MustGet("App")
	db := App.(*app.App).DB

	activityUUID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	activity, err := db.GetActivity(activityUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	bearerToken := c.Request.Header.Get("Authorization")
	fmt.Println("Token: ", bearerToken)
	if bearerToken != "" {
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
		userID = user.ID
		if activity.Visibility == 0 && activity.User.ID != user.ID {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "You can't access this activity"})
			return
		}
	} else if activity.Visibility != 2 {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "You can't access this activity"})
		return
	}
	if activity.User.ID != userID {
		// Deleting non-public datas if user is not the owner
		activity.GpsPoints = nil
		activity.GpsTimeStamps = nil
		activity.StartPosition = nil
		activity.EndPosition = nil
		activity.User = models.Users{}
		activity.Equipment = models.Equipments{}
		// TODO : is there more fields that needs to be nilled ?

	}
	c.JSON(http.StatusOK, gin.H{"activity": activity})
}

func DeleteActivity(c *gin.Context) {
	App := c.MustGet("App")
	bearerToken := c.Request.Header.Get("Authorization")
	userUUID, err := utils.GetUserUUID(bearerToken)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	db := App.(*app.App).DB
	user, err := db.GetUserByUUID(userUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	activityUUID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	activity, err := db.GetActivity(activityUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if activity.User.ID != user.ID {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "You are not the owner of this activity"})
		return
	}
	err = activityService.DeleteActivity(&db, activity, App.(*app.App).Logger)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": activity.ID.String()})
}

func GetSimilarActivities(c *gin.Context) {
	App := c.MustGet("App")
	bearerToken := c.Request.Header.Get("Authorization")
	userUUID, err := utils.GetUserUUID(bearerToken)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	db := App.(*app.App).DB
	user, err := db.GetUserByUUID(userUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	activityUUID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	activity, err := db.GetActivity(activityUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if activity.User.ID != user.ID {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "You are not the owner of this activity"})
		return
	}

	similar := []types.RouteActivity{}
	var route *models.Route
	if activity.RouteID != nil {
		similar, err = db.GetActivitiesByRoute(*activity.RouteID, activity.ID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		route, err = db.GetRoute(*activity.RouteID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"activities": similar, "route": route})
}

// SearchActivities returns a page of the user's activities:
// ?sport=a&sport=b&from=YYYY-MM-DD&to=YYYY-MM-DD (included, in the user's timezone)&q=title
// &equipment=<id>|none&commute=true|false&sort=date|title|distance|duration|speed|elevation&order=asc|desc
// &page=1&per_page=25 (100 at most)
func SearchActivities(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user, location, ok := currentUserLocation(c)
	if !ok {
		return
	}
	bad := func(message string) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": message})
	}

	filter := models.ActivityFilter{Sports: c.QueryArray("sport"), Query: strings.TrimSpace(c.Query("q"))}
	if from := c.Query("from"); from != "" {
		date, err := time.ParseInLocation(time.DateOnly, from, location)
		if err != nil {
			bad("from must be YYYY-MM-DD")
			return
		}
		filter.From = date
	}
	if to := c.Query("to"); to != "" {
		date, err := time.ParseInLocation(time.DateOnly, to, location)
		if err != nil {
			bad("to must be YYYY-MM-DD")
			return
		}
		filter.To = date.AddDate(0, 0, 1)
	}
	switch equipment := c.Query("equipment"); equipment {
	case "":
	case "none":
		filter.NoEquipment = true
	default:
		id, err := uuid.Parse(equipment)
		if err != nil {
			bad("equipment must be an id or none")
			return
		}
		filter.EquipmentID = &id
	}
	if commute := c.Query("commute"); commute != "" {
		isCommute, err := strconv.ParseBool(commute)
		if err != nil {
			bad("commute must be true or false")
			return
		}
		filter.Commute = &isCommute
	}

	sort := c.DefaultQuery("sort", "date")
	if _, ok := models.SortColumns[sort]; !ok {
		bad("unknown sort " + sort)
		return
	}
	order := c.DefaultQuery("order", "desc")
	if order != "asc" && order != "desc" {
		bad("order must be asc or desc")
		return
	}
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		bad("page must be a positive number")
		return
	}
	perPage, err := strconv.Atoi(c.DefaultQuery("per_page", "25"))
	if err != nil || perPage < 1 || perPage > 100 {
		bad("per_page must be between 1 and 100")
		return
	}

	search, err := db.SearchActivities(user.ID, filter, sort, order == "desc", page, perPage, location)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, search)
}

// SetRouteCommute marks all the activities of a route, and the ones imported later, as commutes or not
func SetRouteCommute(c *gin.Context) {
	var body struct {
		IsCommute *bool `json:"is_commute" binding:"required"`
	}
	App := c.MustGet("App")
	bearerToken := c.Request.Header.Get("Authorization")
	userUUID, err := utils.GetUserUUID(bearerToken)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	db := App.(*app.App).DB
	user, err := db.GetUserByUUID(userUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	routeUUID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	route, err := db.GetRoute(routeUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "unknown route"})
		return
	}
	if route.UserID != user.ID {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "You are not the owner of this route"})
		return
	}

	updated, err := db.SetRouteCommute(route.ID, *body.IsCommute)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"updated": updated})
}

// FindDuplicates returns the user's groups of duplicate activities, see models.FindDuplicates
func FindDuplicates(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user, _, ok := currentUserLocation(c)
	if !ok {
		return
	}
	groups, err := db.FindDuplicates(user.ID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"groups": groups})
}
