package vars

import (
	"strings"
	"testing"
)

func TestCheckSecretKey(t *testing.T) {
	for _, key := range []string{"", "changemetoo", strings.Repeat("x", MinSecretKeyLength-1)} {
		if CheckSecretKey(key) == nil {
			t.Errorf("%q accepted", key)
		}
	}
	if err := CheckSecretKey(strings.Repeat("x", MinSecretKeyLength)); err != nil {
		t.Error(err)
	}
}
