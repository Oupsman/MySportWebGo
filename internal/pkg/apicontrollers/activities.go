package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/notifications"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/services/activityService"
	"context"
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
	"unicode/utf8"
)

// Directory structures :
// /MEDIA/user uuid/Activities/raw fit file
// /MEDIA/user uuid/thumnails/activity uuid.png
//

func UploadActivity(c *gin.Context) {
	var uploadParams types.ActivityUpload

	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)

	// single file
	// file, _ := c.FormFile("file")

	err := c.ShouldBind(&uploadParams)
	if err != nil {
		badRequest(c, err)
		return
	}
	if uploadParams.File == nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "missing file"})
		return
	}
	if !withinQuota(c, user, uploadParams.File.Size) {
		return
	}

	file := uploadParams.File
	item := uploadParams.Item
	// count := uploadParams.Count
	baseDir := filepath.Join(activityService.MediaDir, user.UUID.String(), "Activities")
	if err := os.MkdirAll(baseDir, 0770); err != nil {
		internalError(c, err)
		return
	}

	// analysed from a temporary file: a refused upload must not overwrite the FIT file of an activity
	// with the same file name, nor stay on disk
	tmp, err := os.CreateTemp(baseDir, "upload-*.fit")
	if err != nil {
		internalError(c, err)
		return
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath) // fails harmlessly once the file is moved in place
	if err = c.SaveUploadedFile(file, tmpPath); err != nil {
		// the upload was read when binding: writing it is the server's failure
		internalError(c, err)
		return
	}
	if uploadParams.Sport != "" && !activityService.IsSportName(uploadParams.Sport) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "unknown sport " + strconv.Quote(uploadParams.Sport)})
		return
	}
	// a GPX or TCX file becomes a FIT file, a gzipped one is decompressed
	prepared, err := activityService.PrepareUpload(tmpPath, file.Filename, uploadParams.Sport)
	if err != nil {
		analysisError(c, err)
		return
	}
	if prepared.SourcePath != "" {
		defer os.Remove(prepared.SourcePath) // fails harmlessly once the file is moved in place
	}
	activity, err := activityService.AnalyzeTrimmed(tmpPath, user, 0, activityService.Analysis{
		RestHR:     activityService.UserRestHR(&db, user.ID),
		Correction: activityService.DeviceCorrection(&db, user.ID),
		Weight:     activityService.ImportWeight(&db, user.ID),
		FTP:        activityService.UserFTP(&db, user.ID),
	})
	if err != nil {
		analysisError(c, err)
		return
	}
	activity.UserID = user.ID
	if err := activityService.AssignDefaultEquipment(&db, &activity); err != nil {
		internalError(c, err)
		return
	}
	activity.ID = uuid.New()
	activity.Filename = filepath.Base(file.Filename)

	// checked again when saving; checked here to answer before generating the thumbnail, which is slow
	existing, err := db.FindDuplicateActivity(user.ID, activity.Sport, activity.Date)
	if err != nil {
		internalError(c, err)
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
			internalError(c, err)
			return
		}

	}

	err = activityService.GenerateThumbnail(activity)
	if err != nil {
		if !serverBusy(c, err) {
			internalError(c, err)
		}
		return
	}
	activity.ThumbnailVersion = activityService.ThumbnailVersion

	// the FIT file keeps its name, prefixed by the activity id when another file has it; the GPX or TCX
	// file it was converted from is kept beside it, never served
	store := func(activity *models.Activity) error {
		prefix := ""
		for _, name := range []string{prepared.FITName, prepared.SourceName} {
			if _, err := os.Stat(filepath.Join(baseDir, name)); name != "" && err == nil {
				prefix = activity.ID.String() + "_"
			}
		}
		activity.FilePath = filepath.Join(baseDir, prefix+prepared.FITName)
		if prepared.SourcePath != "" {
			activity.SourceFilePath = filepath.Join(baseDir, prefix+prepared.SourceName)
			if err := os.Rename(prepared.SourcePath, activity.SourceFilePath); err != nil {
				return err
			}
		}
		if err := os.Rename(tmpPath, activity.FilePath); err != nil {
			if activity.SourceFilePath != "" {
				os.Remove(activity.SourceFilePath)
			}
			return err
		}
		return nil
	}
	err = activityService.CreateActivity(&db, &activity, store)
	if err != nil {
		os.Remove(activityService.ThumbnailPath(user.UUID, activity.ID))
		var duplicate *activityService.DuplicateError
		if errors.As(err, &duplicate) {
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": err.Error(), "id": duplicate.ExistingID.String()})
			return
		}
		internalError(c, err)
		return
	}
	// the rides of the weeks after it may get a higher estimated FTP
	App := c.MustGet("App").(*app.App)
	activityService.PowerRideChanged(&db, activity, true, App.Go, App.Logger)
	// its passes on the segments of the user: the activity is saved, a failure only misses them
	if err := activityService.MatchActivitySegments(&db, activity.ID, activityService.MatchUpload); err != nil {
		c.MustGet("App").(*app.App).Logger.Error().Err(err).Str("activity", activity.ID.String()).
			Msg("passes on the segments not found")
	}
	// its weather, from the weather source: in the background, the answer doesn't wait
	App.Go(func(ctx context.Context) {
		if err := activityService.FetchActivityWeather(ctx, &db, activity.ID, activity.Date, activity.StartPosition); err != nil {
			App.Logger.Warn().Err(err).Str("activity", activity.ID.String()).
				Msg("weather not read: asked again at the next start")
		}
	})
	// its new bests, after its passes: in the background, the answer doesn't wait
	App.Go(func(context.Context) {
		err := notifications.NotifyHighlights(&db, App.Logger, user.ID, activity.ID, activity.Title, activity.Date,
			activityService.UserLocation(user.Timezone), time.Now())
		if err != nil {
			App.Logger.Error().Err(err).Str("activity", activity.ID.String()).Msg("new bests not notified")
		}
	})

	c.JSON(http.StatusOK, gin.H{"id": activity.ID.String()})
}

func ListActivities(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)
	start, err := strconv.Atoi((c.Param("start")))
	if err != nil {
		badRequest(c, err)
		return
	}
	count, err := strconv.Atoi(c.Param("count"))
	if err != nil {
		badRequest(c, err)
		return
	}

	activities, err := db.GetActivitiesByUser(user.ID, start, count)
	if err != nil {
		internalError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"activities": activities})
}

func UpdateActivity(c *gin.Context) {
	var newActivity models.Activity

	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)

	activityUUID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return
	}

	activity, err := db.GetActivity(activityUUID)
	if err != nil {
		lookupError(c, err, "activity")
		return
	}
	if activity.User.ID != user.ID {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "You are not the owner of this activity"})
		return
	}
	err = c.ShouldBindJSON(&newActivity)
	if err != nil {
		badRequest(c, err)
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
	newActivity.EquipmentID, err = activityService.EquipmentOnUpdate(&db, *activity, newActivity.IsCommute, newActivity.EquipmentID)
	if err != nil {
		internalError(c, err)
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
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": activity.ID.String()})
}

// MaxTitleLength is the longest title of an activity, in characters
const MaxTitleLength = 200

// SetActivityTitle renames an activity, and changes nothing else: the clients importing titles from
// elsewhere (the desktop application, from a Strava export) leave the other fields alone
func SetActivityTitle(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)
	activityUUID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return
	}
	var body struct {
		Title string `json:"title"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, err)
		return
	}
	title := strings.TrimSpace(body.Title)
	if title == "" || utf8.RuneCountInString(title) > MaxTitleLength {
		c.AbortWithStatusJSON(http.StatusBadRequest,
			gin.H{"error": fmt.Sprintf("the title must have 1 to %d characters", MaxTitleLength)})
		return
	}
	renamed, err := db.RenameActivity(user.ID, activityUUID, title)
	if err != nil {
		internalError(c, err)
		return
	}
	if !renamed {
		// the same answer whether the activity exists or belongs to another user
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "activity not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": activityUUID.String(), "title": title})
}

func GetActivity(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB

	activityUUID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return
	}
	activity, err := db.GetActivity(activityUUID)
	if err != nil {
		lookupError(c, err, "activity")
		return
	}

	// user is empty (ID 0) for an anonymous request
	user, authenticated := middlewares.LookupUser(c)
	if !mayView(activity.Visibility, authenticated && activity.User.ID == user.ID, authenticated) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "You can't access this activity"})
		return
	}
	if activity.User.ID != user.ID {
		activity.HidePrivate()
	} else if activity.DeviceID != nil {
		// the name the owner gave the device is theirs: the others see the one of the file
		if activity.DeviceLabel, err = db.DeviceLabel(*activity.DeviceID); err != nil {
			internalError(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"activity": activity})
}

func DeleteActivity(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	db := App.DB
	user := middlewares.CurrentUser(c)

	activityUUID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return
	}

	activity, err := db.GetActivity(activityUUID)
	if err != nil {
		lookupError(c, err, "activity")
		return
	}
	if activity.User.ID != user.ID {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "You are not the owner of this activity"})
		return
	}
	err = activityService.DeleteActivity(&db, activity, App.Logger)
	if err != nil {
		internalError(c, err)
		return
	}
	// the rides of the weeks after it may lose the FTP estimated from it
	activityService.PowerRideChanged(&db, *activity, false, App.Go, App.Logger)
	c.JSON(http.StatusOK, gin.H{"id": activity.ID.String()})
}

// TrimActivity ignores what was recorded after "end" seconds since the start of the activity, when the
// user forgot to stop the device; 0 restores the whole recording
func TrimActivity(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	db := App.DB
	user := middlewares.CurrentUser(c)

	activityUUID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return
	}
	var params struct {
		End *float64 `json:"end" binding:"required"`
	}
	if err := c.ShouldBindJSON(&params); err != nil {
		badRequest(c, err)
		return
	}
	if *params.End < 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid end"})
		return
	}
	activity, err := db.GetActivity(activityUUID)
	if err != nil {
		lookupError(c, err, "activity")
		return
	}
	if activity.User.ID != user.ID {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "You are not the owner of this activity"})
		return
	}
	trimmed, err := activityService.TrimActivity(&db, activity, *params.End, App.Logger)
	if err != nil {
		App.Logger.Warn().Err(err).Str("activity", activity.ID.String()).Float64("end", *params.End).Msg("activity not trimmed")
		analysisError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": trimmed.ID.String(), "trim_end": trimmed.TrimEnd})
}

func GetSimilarActivities(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)

	activityUUID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return
	}
	activity, err := db.GetActivity(activityUUID)
	if err != nil {
		lookupError(c, err, "activity")
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
			internalError(c, err)
			return
		}
		route, err = db.GetRoute(*activity.RouteID)
		if err != nil {
			internalError(c, err)
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"activities": similar, "route": route})
}

// SearchActivities returns a page of the user's activities:
// ?sport=a&sport=b&from=YYYY-MM-DD&to=YYYY-MM-DD (included, in the user's timezone)&q=title
// &equipment=<id>|none&device=<name>|none&device_id=<id>&commute=true|false&sort=date|title|distance|duration|speed|elevation&order=asc|desc
// &page=1&per_page=25 (100 at most)
func SearchActivities(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user, location := currentUserLocation(c)
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
	switch device := c.Query("device"); device {
	case "":
	case "none":
		filter.NoDevice = true
	default:
		filter.Device = device
	}
	if device := c.Query("device_id"); device != "" {
		id, err := uuid.Parse(device)
		if err != nil {
			bad("device_id must be an id")
			return
		}
		filter.DeviceID = &id
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
		internalError(c, err)
		return
	}
	for i, activity := range search.Activities {
		if activity.HasMap {
			search.Activities[i].Thumbnail = activityService.ThumbnailURL(user.UUID, activity.ID)
		}
	}
	c.JSON(http.StatusOK, search)
}

// SetRouteCommute marks all the activities of a route, and the ones imported later, as commutes or not
func SetRouteCommute(c *gin.Context) {
	var body struct {
		IsCommute *bool `json:"is_commute" binding:"required"`
	}
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)

	routeUUID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, err)
		return
	}
	route, err := db.GetRoute(routeUUID)
	if err != nil {
		lookupError(c, err, "route")
		return
	}
	if route.UserID != user.ID {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "You are not the owner of this route"})
		return
	}

	updated, err := db.SetRouteCommute(route.ID, *body.IsCommute)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"updated": updated})
}

// FindDuplicates returns the user's groups of duplicate activities, see models.FindDuplicates
func FindDuplicates(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)
	groups, err := db.FindDuplicates(user.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"groups": groups})
}
