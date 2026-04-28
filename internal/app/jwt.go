package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	DummyAdminID = "11111111-1111-4111-8111-111111111111"
	DummyUserID  = "22222222-2222-4222-8222-222222222222"
)

type jwtClaims struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

func makeToken(secret []byte, userID uuid.UUID, role string, ttl time.Duration) (string, error) {
	now := time.Now().UTC()
	c := jwtClaims{
		UserID: userID.String(),
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return t.SignedString(secret)
}

func readToken(secret []byte, token string) (jwtClaims, error) {
	var c jwtClaims
	_, err := jwt.ParseWithClaims(token, &c, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("bad method")
		}
		return secret, nil
	})
	if err != nil {
		return jwtClaims{}, err
	}
	if c.UserID == "" || (c.Role != "admin" && c.Role != "user") {
		return jwtClaims{}, errors.New("bad claims")
	}
	return c, nil
}

func dummyUserID(role string) (uuid.UUID, error) {
	switch role {
	case "admin":
		return uuid.MustParse(DummyAdminID), nil
	case "user":
		return uuid.MustParse(DummyUserID), nil
	default:
		return uuid.Nil, fmt.Errorf("bad role")
	}
}

func DummyUserIDForIntegration(role string) (uuid.UUID, error) {
	return dummyUserID(role)
}
