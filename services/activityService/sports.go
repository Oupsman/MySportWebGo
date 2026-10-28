package activityService

import (
	"strings"

	"github.com/muktihari/fit/profile/typedef"
)

// SportName returns the name of a FIT sport, "running", "cross_country_skiing"... A sport missing from the
// file, or more recent than the FIT profile of the library, is "generic": the activity is kept rather than
// refused, the analysis of a later version of the library can name it.
func SportName(sport typedef.Sport) string {
	if sport == typedef.SportAll || sport == typedef.SportInvalid {
		return typedef.SportGeneric.String()
	}
	name := sport.String()
	if strings.HasPrefix(name, "SportInvalid") {
		return typedef.SportGeneric.String()
	}
	return name
}
