package utils

import (
	"MySportWeb/internal/pkg/types"
	"testing"
)

// the example of Google's documentation of the format
func TestEncodePolyline(t *testing.T) {
	points := []types.GpsPoint{{Lat: 38.5, Lon: -120.2}, {Lat: 40.7, Lon: -120.95}, {Lat: 43.252, Lon: -126.453}}
	if got := EncodePolyline(points); got != "_p~iF~ps|U_ulLnnqC_mqNvxq`@" {
		t.Errorf("got %q", got)
	}
	if got := EncodePolyline(nil); got != "" {
		t.Errorf("empty: %q", got)
	}
}
