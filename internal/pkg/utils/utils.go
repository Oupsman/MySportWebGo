package utils

import (
	"MySportWeb/internal/pkg/vars"
	"github.com/dgrijalva/jwt-go"
	"golang.org/x/crypto/bcrypt"
	"math"
	"strings"
)

func GenerateHashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

func CompareHashPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func ParseToken(tokenString string) (claims jwt.MapClaims, err error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return []byte(vars.SecretKey), nil
	})

	if err != nil {
		return nil, err
	}

	claims = token.Claims.(jwt.MapClaims)
	/*if !ok {
		return nil, err
	}*/

	return claims, nil
}

func GetUserID(tokenString string) (uint64, error) {
	reqToken := strings.Split(tokenString, " ")[1]

	claims, err := ParseToken(reqToken)
	if err != nil {
		return 0, err
	}
	return uint64(claims["sub"].(float64)), nil
}

func StringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func GetUserRole(tokenString string) (string, error) {
	reqToken := strings.Split(tokenString, " ")[1]

	claims, err := ParseToken(reqToken)
	if err != nil {
		return "", err
	}
	return claims["role"].(string), nil
}

func SemiCircleToDegres(semi float64) float64 {
	if semi > 0 {
		return semi * (180.0 / math.Pow(2.0, 31.0))
	}
	return 0
}
