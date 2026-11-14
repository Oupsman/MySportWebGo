package apicontrollers

import (
	"MySportWeb/services/activityService"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

const uuidPattern = `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`

// publicMedia are the files of MEDIA/ the browsers load in <img> tags, which can't send the Authorization
// header: the thumbnails, drawn from the public trace only, and the equipment pictures, re-encoded without
// metadata. Their names are random UUIDs. The FIT files, holding the whole trace, are never served.
var publicMedia = regexp.MustCompile(`^` + uuidPattern + `/(thumbnails/` + uuidPattern + `\.png|equipments/` +
	uuidPattern + `/` + uuidPattern + `\.jpg)$`)

// ServeMedia serves the files of MEDIA/ matching publicMedia, 404 for the others: no directory listing,
// no FIT file. The pattern allows no "..", the path can't leave MEDIA/.
func ServeMedia(c *gin.Context) {
	path := strings.TrimPrefix(c.Param("path"), "/")
	if !publicMedia.MatchString(path) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.Header("X-Content-Type-Options", "nosniff")
	c.File(filepath.Join(activityService.MediaDir, filepath.FromSlash(path)))
}
