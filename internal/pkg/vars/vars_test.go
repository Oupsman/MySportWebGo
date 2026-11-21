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

func TestParseRegistration(t *testing.T) {
	for value, want := range map[string]RegistrationMode{
		"": RegistrationClosed, "closed": RegistrationClosed, "invite": RegistrationInvite,
		"open": RegistrationOpen, " Open ": RegistrationOpen,
	} {
		if got, err := ParseRegistration(value); err != nil || got != want {
			t.Errorf("%q: %q %v, want %q", value, got, err, want)
		}
	}
	for _, value := range []string{"yes", "true", "invitation"} {
		if _, err := ParseRegistration(value); err == nil {
			t.Errorf("%q accepted", value)
		}
	}
}
