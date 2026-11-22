package utils

import (
	"MySportWeb/internal/pkg/vars"
	"errors"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// SessionLifetime is how long a session token of the web application is valid
const SessionLifetime = 30 * 24 * time.Hour

// ChallengeLifetime is how long the second step of a login with two-factor authentication can wait
const ChallengeLifetime = 5 * time.Minute

// Session is what a session token tells: which session (to revoke it), of whom, since and until when
type Session struct {
	ID        string
	UserUUID  uuid.UUID
	IssuedAt  time.Time
	ExpiresAt time.Time
	// MFA tells the login went through the two-factor authentication: the admin routes require it
	MFA bool
}

// NewSessionToken signs a session token for a user, issued at now, without second factor
func NewSessionToken(userID uint, userUUID uuid.UUID, role string, now time.Time) (string, Session, error) {
	return NewSessionTokenMFA(userID, userUUID, role, now, false)
}

// NewSessionTokenMFA signs a session token for a user, issued at now, mfa when the login went through the
// two-factor authentication
func NewSessionTokenMFA(userID uint, userUUID uuid.UUID, role string, now time.Time, mfa bool) (string, Session, error) {
	session := Session{
		ID:        uuid.NewString(),
		UserUUID:  userUUID,
		IssuedAt:  now.Truncate(time.Second),
		ExpiresAt: now.Add(SessionLifetime).Truncate(time.Second),
		MFA:       mfa,
	}
	claims := jwt.MapClaims{
		"authorized": true,
		"role":       role,
		"iat":        session.IssuedAt.Unix(),
		"exp":        session.ExpiresAt.Unix(),
		"iss":        "mysportweb",
		"jti":        session.ID,
		"sub":        userID,
		"uuid":       userUUID.String(),
		"mfa":        mfa,
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(vars.SecretKey))
	return token, session, err
}

// challengePurpose marks the tokens of the second step of a login: never a session
const challengePurpose = "2fa"

// NewChallengeToken signs the token of the second step of a login, for a user whose password was checked
func NewChallengeToken(userUUID uuid.UUID, now time.Time) (string, error) {
	claims := jwt.MapClaims{
		"purpose": challengePurpose,
		"iat":     now.Unix(),
		"exp":     now.Add(ChallengeLifetime).Unix(),
		"iss":     "mysportweb",
		"uuid":    userUUID.String(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(vars.SecretKey))
}

// ParseChallengeToken returns the user of a token of NewChallengeToken, an error when expired or not one
func ParseChallengeToken(token string) (uuid.UUID, error) {
	claims, err := ParseToken(token)
	if err != nil {
		return uuid.Nil, err
	}
	if purpose, _ := claims["purpose"].(string); purpose != challengePurpose {
		return uuid.Nil, errors.New("not a two-factor challenge")
	}
	userUUID, _ := claims["uuid"].(string)
	return uuid.Parse(userUUID)
}

// ParseSession returns the session of the token of an Authorization header, see ParseSessionToken
func ParseSession(header string) (Session, error) {
	token, err := BearerToken(header)
	if err != nil {
		return Session{}, err
	}
	return ParseSessionToken(token)
}

// ParseSessionToken returns the session of a session token. The tokens without id or issue date (signed
// before sessions could be revoked) are refused: their users log in again.
func ParseSessionToken(token string) (Session, error) {
	claims, err := ParseToken(token)
	if err != nil {
		return Session{}, err
	}
	// a challenge of the second step of a login is no session
	if purpose, _ := claims["purpose"].(string); purpose != "" {
		return Session{}, errors.New("not a session token")
	}
	id, _ := claims["jti"].(string)
	if id == "" {
		return Session{}, errors.New("session token without id, log in again")
	}
	issuedAt, err := claims.GetIssuedAt()
	if err != nil || issuedAt == nil {
		return Session{}, errors.New("session token without issue date, log in again")
	}
	expiresAt, err := claims.GetExpirationTime()
	if err != nil || expiresAt == nil {
		return Session{}, errors.New("invalid token expiration")
	}
	userUUID, ok := claims["uuid"].(string)
	if !ok {
		return Session{}, errors.New("invalid token uuid")
	}
	parsed, err := uuid.Parse(userUUID)
	if err != nil {
		return Session{}, err
	}
	mfa, _ := claims["mfa"].(bool)
	return Session{ID: id, UserUUID: parsed, IssuedAt: issuedAt.Time, ExpiresAt: expiresAt.Time, MFA: mfa}, nil
}

var (
	dummyHashOnce sync.Once
	dummyHash     string
)

// CheckPasswordOfNobody takes as long as checking a password: a login with an unknown username answers
// as slowly as one with a wrong password, the time doesn't tell which usernames exist
func CheckPasswordOfNobody(password string) {
	dummyHashOnce.Do(func() {
		dummyHash, _ = GenerateHashPassword("no user has this password")
	})
	_ = CheckPassword(password, dummyHash)
}
