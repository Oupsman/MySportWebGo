package utils

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckNewPassword(t *testing.T) {
	for password, want := range map[string]error{
		"correct horse":             nil,
		"Tr4il-runn3r":              nil,
		"éléphant!!":                nil, // 10 characters, more bytes
		"short":                     ErrPasswordTooShort,
		"123456789":                 ErrPasswordTooShort,
		strings.Repeat("é", 37):     ErrPasswordTooLong, // 37 characters, 74 bytes
		"QwertyUIOP":                ErrPasswordCommon,
		"password123":               ErrPasswordCommon,
		"aaaaaaaaaaaa":              ErrPasswordPattern,
		"abcdefghijk":               ErrPasswordPattern,
		"9876543210":                ErrPasswordPattern,
		"876543210/":                ErrPasswordPattern,
		"Rider2026!":                ErrPasswordIdentity, // the username and a year
		"jane.doe@mail.test":        ErrPasswordIdentity,
		"jane.doe123":               ErrPasswordIdentity, // the local part of the email and digits
		"jane.doe likes long rides": nil,                 // the local part is only a part of it
	} {
		err := CheckNewPassword(password, "rider", "jane.doe@mail.test")
		if !errors.Is(err, want) {
			t.Errorf("%q: %v, want %v", password, err, want)
		}
	}
	// no identities: a reset link doesn't know them yet
	if err := CheckNewPassword("rider2026!"); err != nil {
		t.Errorf("without identities: %v", err)
	}
}
