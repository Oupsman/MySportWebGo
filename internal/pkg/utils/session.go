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

// Session is what a session token tells: which session (to revoke it), of whom, since and until when
type Session struct {
	ID        string
	UserUUID  uuid.UUID
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// NewSessionToken signs a session token for a user, issued at now
func NewSessionToken(userID uint, userUUID uuid.UUID, role string, now time.Time) (string, Session, error) {
	session := Session{
		ID:        uuid.NewString(),
		UserUUID:  userUUID,
		IssuedAt:  now.Truncate(time.Second),
		ExpiresAt: now.Add(SessionLifetime).Truncate(time.Second),
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
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(vars.SecretKey))
	return token, session, err
}

// ParseSession returns the session of the token of an Authorization header. The tokens without id or
// issue date (signed before sessions could be revoked) are refused: their users log in again.
func ParseSession(header string) (Session, error) {
	claims, err := ParseBearerToken(header)
	if err != nil {
		return Session{}, err
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
	return Session{ID: id, UserUUID: parsed, IssuedAt: issuedAt.Time, ExpiresAt: expiresAt.Time}, nil
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
