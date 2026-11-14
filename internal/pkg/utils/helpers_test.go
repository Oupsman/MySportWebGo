package utils

// Used by the tests only (the sessions are read by ParseSession): in a test file, they stay out of the
// compiled code.

import (
	"errors"

	"github.com/google/uuid"
)

func GetUserID(tokenString string) (uint64, error) {
	claims, err := ParseBearerToken(tokenString)
	if err != nil {
		return 0, err
	}
	sub, ok := claims["sub"].(float64)
	if !ok {
		return 0, errors.New("invalid token subject")
	}
	return uint64(sub), nil
}

func GetUserUUID(tokenString string) (uuid.UUID, error) {
	claims, err := ParseBearerToken(tokenString)
	if err != nil {
		return uuid.UUID{}, err
	}
	userUUID, ok := claims["uuid"].(string)
	if !ok {
		return uuid.UUID{}, errors.New("invalid token uuid")
	}
	return uuid.Parse(userUUID)
}

func GetUserRole(tokenString string) (string, error) {
	claims, err := ParseBearerToken(tokenString)
	if err != nil {
		return "", err
	}
	role, ok := claims["role"].(string)
	if !ok {
		return "", errors.New("invalid token role")
	}
	return role, nil
}
