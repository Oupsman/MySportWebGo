package utils

import (
	"MySportWeb/internal/pkg/vars"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"image/png"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const (
	// TOTPIssuer names the application in the authenticator apps
	TOTPIssuer = "MySportWeb"
	totpPeriod = 30
	// totpSkew is the number of periods before and after now a code is accepted from: the clocks drift
	totpSkew = 1
	// RecoveryCodes is the number of single-use codes given when the two-factor authentication is enabled
	RecoveryCodes = 10
)

// TOTPKey is a new secret of the two-factor authentication: what the authenticator app scans or is typed
type TOTPKey struct {
	Secret string // base32, the one stored (encrypted)
	URL    string // otpauth://, what the QR code holds
	QRCode string // data:image/png;base64,...
}

// NewTOTPKey generates a secret for an account (its username)
func NewTOTPKey(account string) (TOTPKey, error) {
	key, err := totp.Generate(totp.GenerateOpts{Issuer: TOTPIssuer, AccountName: account, Period: totpPeriod,
		Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		return TOTPKey{}, err
	}
	img, err := key.Image(240, 240)
	if err != nil {
		return TOTPKey{}, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return TOTPKey{}, err
	}
	return TOTPKey{Secret: key.Secret(), URL: key.URL(),
		QRCode: "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())}, nil
}

// TOTPStep returns the period of the code a secret gives at now, false when the code isn't the one of now
// or of the totpSkew periods around it. The step tells a code already used (replayed) from a new one.
func TOTPStep(secret, code string, now time.Time) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != 6 {
		return 0, false
	}
	step := now.Unix() / totpPeriod
	for delta := int64(-totpSkew); delta <= totpSkew; delta++ {
		at := time.Unix((step+delta)*totpPeriod, 0)
		expected, err := totp.GenerateCodeCustom(secret, at, totp.ValidateOpts{Period: totpPeriod,
			Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
		if err == nil && subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return step + delta, true
		}
	}
	return 0, false
}

// secretKey is the AES key of the secrets stored in the database, derived from SECRET_KEY: a dump of the
// database alone doesn't give the second factor
func secretKey() []byte {
	key := sha256.Sum256([]byte("mysportweb totp secret\x00" + vars.SecretKey))
	return key[:]
}

// EncryptSecret encrypts a secret to store (AES-GCM), base64
func EncryptSecret(secret string) (string, error) {
	block, err := aes.NewCipher(secretKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(secret), nil)), nil
}

// DecryptSecret decrypts a secret of EncryptSecret: an error when SECRET_KEY changed since
func DecryptSecret(stored string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(secretKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("invalid stored secret")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
	if err != nil {
		return "", errors.New("the two-factor secret can't be decrypted: SECRET_KEY changed? Reset it with " +
			"mysportweb admin reset-2fa")
	}
	return string(plain), nil
}

// NewRecoveryCodes returns single-use codes, shown once, and their hashes, stored
func NewRecoveryCodes() ([]string, []string) {
	codes := make([]string, RecoveryCodes)
	hashes := make([]string, RecoveryCodes)
	for i := range codes {
		code := NewCode()[:10]
		codes[i] = code[:5] + "-" + code[5:]
		hashes[i] = HashRecoveryCode(codes[i])
	}
	return codes, hashes
}

// HashRecoveryCode returns the stored form of a recovery code, its dash, spaces and case ignored
func HashRecoveryCode(code string) string {
	return HashCode(strings.NewReplacer("-", "", " ", "").Replace(code))
}
