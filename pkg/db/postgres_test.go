package db_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eagle-go/eagle/pkg/db"
)

func TestSQLState(t *testing.T) {
	err := fmt.Errorf("insert product: %w", &pgconn.PgError{Code: db.SQLStateUniqueViolation})
	if got := db.SQLState(err); got != db.SQLStateUniqueViolation {
		t.Fatalf("SQLState() = %q", got)
	}
	if !db.IsUniqueViolation(err) {
		t.Fatal("wrapped unique violation was not recognized")
	}
	if db.IsForeignKeyViolation(err) {
		t.Fatal("unique violation recognized as foreign key violation")
	}
	if got := db.SQLState(errors.New("plain error")); got != "" {
		t.Fatalf("plain error SQLState() = %q", got)
	}
}
