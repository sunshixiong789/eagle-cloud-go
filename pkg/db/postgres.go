package db

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	SQLStateUniqueViolation     = "23505"
	SQLStateForeignKeyViolation = "23503"
)

// SQLState 返回 PostgreSQL 标准错误码，非 PostgreSQL 错误返回空串。
func SQLState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func IsUniqueViolation(err error) bool {
	return SQLState(err) == SQLStateUniqueViolation
}

func IsForeignKeyViolation(err error) bool {
	return SQLState(err) == SQLStateForeignKeyViolation
}
