// Command migrate is the deployment-time schema migration job shared by all
// services. The selected directory determines database ownership.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func main() {
	dir := flag.String("dir", "", "service migration directory")
	dsn := flag.String("dsn", os.Getenv("EAGLE_DATABASE_DSN"), "PostgreSQL DSN")
	command := flag.String("command", "up", "goose command: up, down, status")
	flag.Parse()
	if *dir == "" || *dsn == "" {
		panic("migrate: -dir and -dsn/EAGLE_DATABASE_DSN are required")
	}
	db, err := sql.Open("pgx", *dsn)
	if err != nil {
		panic(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.PingContext(context.Background()); err != nil {
		panic(err)
	}
	if err := goose.SetDialect("postgres"); err != nil {
		panic(err)
	}
	switch *command {
	case "up":
		err = goose.Up(db, *dir)
	case "down":
		err = goose.Down(db, *dir)
	case "status":
		err = goose.Status(db, *dir)
	default:
		err = fmt.Errorf("unsupported command %q", *command)
	}
	if err != nil {
		panic(err)
	}
}
