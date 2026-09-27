// Package database owns the service Ent client. Business repositories stay
// inside their modules.
package database

import (
	"context"
	"database/sql"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	"github.com/eagle-go/eagle/app/admin/internal/platform/database/ent"
	"github.com/eagle-go/eagle/pkg/db"
	"github.com/eagle-go/eagle/pkg/healthx"
	"github.com/eagle-go/eagle/pkg/platform/config"
)

type Database struct {
	client *ent.Client
	sql    *sql.DB
}

func Open(c *config.Data) (*Database, func(), error) {
	sqlDB, cleanup, err := db.NewMonitored(context.Background(), dbConfig(c), healthx.Default)
	if err != nil {
		return nil, nil, err
	}
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, sqlDB)))
	return &Database{client: client, sql: sqlDB}, cleanup, nil
}

func dbConfig(c *config.Data) db.Config {
	database := c.GetDatabase()
	return db.Config{
		DSN:             database.GetDsn(),
		MaxConns:        database.GetMaxConns(),
		MaxIdleConns:    database.GetMaxIdleConns(),
		MaxConnLifetime: database.GetMaxConnLifetime().AsDuration(),
		MaxConnIdleTime: database.GetMaxConnIdleTime().AsDuration(),
	}
}

func (d *Database) Client() *ent.Client { return d.client }
func (d *Database) SQL() *sql.DB        { return d.sql }

func IsNotFound(err error) bool { return ent.IsNotFound(err) }

func IsUniqueViolation(err error) bool {
	return db.IsUniqueViolation(err) || ent.IsConstraintError(err) && db.SQLState(err) == ""
}

func IsForeignKeyViolation(err error) bool { return db.IsForeignKeyViolation(err) }
