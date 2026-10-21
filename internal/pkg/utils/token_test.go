package utils

import (
	"MySportWeb/internal/pkg/vars"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testUUID = "3c664c0d-a791-489c-a33a-fbce04e43184"

func signToken(t *testing.T, method jwt.SigningMethod, key interface{}, claims jwt.MapClaims) string {
	token, err := jwt.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// validClaims are the claims set by the Login controller
func validClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"authorized": true,
		"role":       "User",
		"exp":        time.Now().Add(time.Hour).Unix(),
		"iss":        "mysportweb",
		"sub":        42,
		"uuid":       testUUID,
	}
}

func TestParseTokenAcceptsLoginTokens(t *testing.T) {
	vars.SecretKey = "testsecret"
	header := "Bearer " + signToken(t, jwt.SigningMethodHS256, []byte(vars.SecretKey), validClaims())

	id, err := GetUserID(header)
	if err != nil || id != 42 {
		t.Fatalf("GetUserID = %d, %v", id, err)
	}
	userUUID, err := GetUserUUID(header)
	if err != nil || userUUID.String() != testUUID {
		t.Fatalf("GetUserUUID = %s, %v", userUUID, err)
	}
	role, err := GetUserRole(header)
	if err != nil || role != "User" {
		t.Fatalf("GetUserRole = %s, %v", role, err)
	}
}

func TestParseTokenRejects(t *testing.T) {
	vars.SecretKey = "testsecret"
	key := []byte(vars.SecretKey)
	with := func(change func(jwt.MapClaims)) jwt.MapClaims {
		claims := validClaims()
		change(claims)
		return claims
	}
	none, err := jwt.NewWithClaims(jwt.SigningMethodNone, validClaims()).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}

	tests := map[string]string{
		"wrong secret":     signToken(t, jwt.SigningMethodHS256, []byte("other"), validClaims()),
		"other algorithm":  signToken(t, jwt.SigningMethodHS512, key, validClaims()),
		"none algorithm":   none,
		"expired":          signToken(t, jwt.SigningMethodHS256, key, with(func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Minute).Unix() })),
		"no expiration":    signToken(t, jwt.SigningMethodHS256, key, with(func(c jwt.MapClaims) { delete(c, "exp") })),
		"other issuer":     signToken(t, jwt.SigningMethodHS256, key, with(func(c jwt.MapClaims) { c["iss"] = "someone" })),
		"not a token":      "garbage",
		"tampered payload": signToken(t, jwt.SigningMethodHS256, key, validClaims()) + "x",
	}
	for name, token := range tests {
		if _, err := ParseToken(token); err == nil {
			t.Errorf("%s: token accepted", name)
		}
	}
}

func TestBearerToken(t *testing.T) {
	for _, header := range []string{"", "Bearer", "Bearer ", "Basic abc", "bearer abc"} {
		if _, err := BearerToken(header); err == nil {
			t.Errorf("%q: accepted", header)
		}
	}
	if token, err := BearerToken("Bearer abc"); err != nil || token != "abc" {
		t.Errorf("Bearer abc = %q, %v", token, err)
	}
}

func TestClaimsWithWrongTypes(t *testing.T) {
	vars.SecretKey = "testsecret"
	claims := validClaims()
	claims["uuid"] = 12
	claims["sub"] = "42"
	header := "Bearer " + signToken(t, jwt.SigningMethodHS256, []byte(vars.SecretKey), claims)
	if _, err := GetUserUUID(header); err == nil {
		t.Error("non string uuid accepted")
	}
	if _, err := GetUserID(header); err == nil {
		t.Error("non numeric sub accepted")
	}
}
