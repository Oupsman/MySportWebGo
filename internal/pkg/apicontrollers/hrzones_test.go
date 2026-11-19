package apicontrollers

import (
	"MySportWeb/internal/pkg/models"
	"testing"
	"time"
)

func TestMaxHRChanged(t *testing.T) {
	birth := time.Date(1980, 5, 17, 0, 0, 0, 0, time.UTC)
	user := models.Users{MaxHR: 185, DateOfBirth: birth}
	same, other := uint16(185), uint16(180)
	// the same instant sent in another timezone
	sameBirth := birth.In(time.FixedZone("CEST", 2*3600))
	otherBirth := birth.AddDate(1, 0, 0)
	cases := []struct {
		name   string
		update userUpdate
		want   bool
	}{
		{"nothing about it", userUpdate{}, false},
		{"the same maximum", userUpdate{MaxHR: &same}, false},
		{"another maximum", userUpdate{MaxHR: &other}, true},
		{"the same date of birth", userUpdate{DateOfBirth: &sameBirth}, false},
		{"another date of birth", userUpdate{DateOfBirth: &otherBirth}, true},
	}
	for _, c := range cases {
		if got := maxHRChanged(user, c.update); got != c.want {
			t.Errorf("%s: changed %v, want %v", c.name, got, c.want)
		}
	}
}
