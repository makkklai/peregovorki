package app

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsPgUnique(t *testing.T) {
	var e *pgconn.PgError = &pgconn.PgError{Code: "23505"}
	if !isPgUnique(e) {
		t.Fatal()
	}
	if isPgUnique(errors.New("x")) {
		t.Fatal()
	}
}

func TestIsForbiddenCancelExported(t *testing.T) {
	if !IsForbiddenCancel(errForbidden) {
		t.Fatal()
	}
}
