package app

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMakeAndReadToken(t *testing.T) {
	sec := []byte("test-secret-test-secret-test-se")
	uid := uuid.MustParse(DummyUserID)
	tok, err := makeToken(sec, uid, "user", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	c, err := readToken(sec, tok)
	if err != nil || c.UserID != uid.String() || c.Role != "user" {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestDummyUserID(t *testing.T) {
	a, _ := dummyUserID("admin")
	if a.String() != DummyAdminID {
		t.Fatal(a)
	}
	if _, err := dummyUserID("x"); err == nil {
		t.Fatal("expected err")
	}
}
