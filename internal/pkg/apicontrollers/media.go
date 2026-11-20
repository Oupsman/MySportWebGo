package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/services/activityService"
	"errors"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const uuidPattern = `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`

// servedMedia are the files of MEDIA/ the browsers load in <img> tags: the thumbnails, drawn from the public
// trace only, and the equipment pictures, re-encoded without metadata. The FIT files, holding the whole
// trace, are never served. Groups: the owner, then the activity of a thumbnail ("" for a picture).
var servedMedia = regexp.MustCompile(`^(` + uuidPattern + `)/(?:thumbnails/(` + uuidPattern + `)\.png|equipments/` +
	uuidPattern + `/` + uuidPattern + `\.jpg)$`)

// The visibility of an activity
const (
	visibilityPrivate = 0 // its owner only
	visibilityUsers   = 1 // the logged in users
	visibilityPublic  = 2 // anyone
)

// mayView tells whether the user of a request (authenticated false: anonymous) may see an activity of a
// visibility
func mayView(visibility int, owner, authenticated bool) bool {
	return owner || visibility == visibilityPublic || (visibility == visibilityUsers && authenticated)
}

// ServeMedia serves the files of MEDIA/ matching servedMedia to whom may see them, behind OptionalUser (the
// session cookie: an <img> can't send the Authorization header):
//   - a thumbnail to whom may see its activity (mayView): its owner, the others by its visibility
//   - an equipment picture to its owner
//
// 404 for anything else, a file that exists or not: the answer doesn't tell. The pattern allows no "..",
// the path can't leave MEDIA/.
func ServeMedia(c *gin.Context) {
	path := strings.TrimPrefix(c.Param("path"), "/")
	match := servedMedia.FindStringSubmatch(path)
	if match == nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	user, authenticated := middlewares.LookupUser(c)
	allowed := authenticated && user.UUID.String() == match[1]
	if !allowed && match[2] != "" {
		var err error
		if allowed, err = thumbnailAllowed(c, match[1], match[2], authenticated); err != nil {
			internalError(c, err)
			return
		}
	}
	if !allowed {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.Header("X-Content-Type-Options", "nosniff")
	// per user, and a thumbnail is drawn again when its activity is trimmed: the browser keeps it, but
	// asks each time whether it changed (Last-Modified, 304)
	c.Header("Cache-Control", "private, no-cache")
	c.File(filepath.Join(activityService.MediaDir, filepath.FromSlash(path)))
}

// thumbnailAllowed tells whether the thumbnail of an activity, in the directory of owner, may be seen by
// someone else than its owner
func thumbnailAllowed(c *gin.Context, owner, activity string, authenticated bool) (bool, error) {
	db := c.MustGet("App").(*app.App).DB
	access, err := db.GetMediaAccess(uuid.MustParse(activity))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// the activity must be the one of the owner of the directory
	return access.OwnerUUID.String() == owner && mayView(access.Visibility, false, authenticated), nil
}
