package utils

import (
	"MySportWeb/internal/pkg/types"
	"math"
	"strings"
)

// EncodePolyline encodes a trace in Google's encoded polyline format, precision 5 (about 1 m): the
// difference of each point to the previous one, 5 bits a character
func EncodePolyline(points []types.GpsPoint) string {
	var b strings.Builder
	b.Grow(len(points) * 6)
	var prevLat, prevLon int64
	for _, p := range points {
		lat, lon := int64(math.Round(p.Lat*1e5)), int64(math.Round(p.Lon*1e5))
		encodeValue(&b, lat-prevLat)
		encodeValue(&b, lon-prevLon)
		prevLat, prevLon = lat, lon
	}
	return b.String()
}

func encodeValue(b *strings.Builder, v int64) {
	u := uint64(v) << 1
	if v < 0 {
		u = ^u
	}
	for u >= 0x20 {
		b.WriteByte(byte((0x20 | (u & 0x1f)) + 63))
		u >>= 5
	}
	b.WriteByte(byte(u + 63))
}
