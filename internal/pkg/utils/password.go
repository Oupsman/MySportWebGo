package utils

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The rules of a new password (sign up, change, reset link). The passwords set before stay valid.
const (
	// MinPasswordLength is the shortest new password, in characters
	MinPasswordLength = 10
	// MaxPasswordBytes is the longest: bcrypt ignores what follows
	MaxPasswordBytes = 72
)

var (
	ErrPasswordTooShort = fmt.Errorf("the password needs at least %d characters", MinPasswordLength)
	ErrPasswordTooLong  = fmt.Errorf("the password is longer than %d bytes", MaxPasswordBytes)
	ErrPasswordCommon   = errors.New("this password is among the most used ones: choose another")
	ErrPasswordIdentity = errors.New("the password can't be your username or your email address")
	ErrPasswordPattern  = errors.New("the password can't be one character repeated, nor a straight sequence")
)

// commonPasswords are frequent passwords of MinPasswordLength characters or more, lowercase: the first ones
// tried. Not a breach list: the length does most of the work.
var commonPasswords = map[string]bool{}

func init() {
	for _, p := range strings.Fields(`
		1234567890 0987654321 12345678910 123456789a a123456789 1234567890a 1q2w3e4r5t 1q2w3e4r5t6y
		1qaz2wsx3edc qazwsxedcrfv zaq12wsxcde3 qwertyuiop qwertyuiop123 qwerty1234 qwerty12345 qwerty123456
		azertyuiop azerty1234 azerty123456 asdfghjkl1 asdfghjkl123 zxcvbnm123 password12 password123
		password1234 password12345 password1! passw0rd123 p@ssw0rd123 motdepasse motdepasse1 motdepasse123
		iloveyou12 iloveyou123 jetaime123 football123 baseball123 basketball soccer1234 superman123
		princess12 sunshine12 welcome123 welcome1234 letmein123 changeme123 administrator admin12345
		admin123456 qwertyqwerty abc1234567 abcd123456 abcdefghij abcdef1234 aaaaaa1111 1111111111a
		myspace123 trustno123 dragon1234 monkey1234 master1234 starwars12 pokemon123 computer12
		internet12 samsung123 michael123 jennifer12 charlie123 liverpool1 chelsea123 arsenal123
		marseille13 olympique1 allezlesbleus mysportweb mysportweb1 mysportweb123 strava1234 garmin1234
		cycling123 running123 triathlon1 marathon123
	`) {
		commonPasswords[p] = true
	}
}

// CheckNewPassword checks a new password: long enough, not too long for bcrypt, not a common one, not one of
// identities (the username, the email address: case ignored), nor a repeated character or a sequence
func CheckNewPassword(password string, identities ...string) error {
	if utf8.RuneCountInString(password) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	if len(password) > MaxPasswordBytes {
		return ErrPasswordTooLong
	}
	lower := strings.ToLower(password)
	if commonPasswords[lower] {
		return ErrPasswordCommon
	}
	for _, identity := range identities {
		identity = strings.ToLower(strings.TrimSpace(identity))
		// the username, or the part of the email before the @, with only digits or symbols added
		local, _, _ := strings.Cut(identity, "@")
		if identity == "" {
			continue
		}
		if lower == identity || (utf8.RuneCountInString(local) >= 4 && strings.HasPrefix(lower, local) &&
			!strings.ContainsFunc(lower[len(local):], unicode.IsLetter)) {
			return ErrPasswordIdentity
		}
	}
	if repeatedOrSequence(lower) {
		return ErrPasswordPattern
	}
	return nil
}

// repeatedOrSequence tells whether text is one character repeated ("aaaaaaaaaa") or each character the
// next or the previous one of the one before ("abcdefghij", "9876543210")
func repeatedOrSequence(text string) bool {
	runes := []rune(text)
	step := runes[1] - runes[0]
	if step < -1 || step > 1 {
		return false
	}
	for i := 2; i < len(runes); i++ {
		if runes[i]-runes[i-1] != step {
			return false
		}
	}
	return true
}
