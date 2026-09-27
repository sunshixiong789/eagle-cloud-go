// Package testsupport provides reusable real-PostgreSQL fixtures for module
// integration tests. It is never imported by production composition.
package testkit

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	_ "github.com/lib/pq"
	"github.com/pressly/goose/v3"
)

type Postgres struct {
	DSN     string
	server  *embeddedpostgres.EmbeddedPostgres
	dataDir string
	port    uint32
}

func StartPostgres(instance, database string, migrationService ...string) (*Postgres, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("locate user directory: %w", err)
	}
	runtimeDir := filepath.Join(home, ".embedded-postgres-go", "eagle-"+instance)
	dataDir, err := os.MkdirTemp("", "eagle-"+instance+"-postgres-")
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL data directory: %w", err)
	}
	port, err := availablePort()
	if err != nil {
		_ = os.RemoveAll(dataDir)
		return nil, err
	}
	server := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Username("eagle").Password("eagle").Database(database).
		Port(port).RuntimePath(runtimeDir).DataPath(dataDir).Logger(io.Discard))
	if err := server.Start(); err != nil {
		_ = os.RemoveAll(dataDir)
		return nil, fmt.Errorf("start embedded postgres: %w", err)
	}
	p := &Postgres{
		DSN:    fmt.Sprintf("postgres://eagle:eagle@127.0.0.1:%d/%s?sslmode=disable", port, database),
		server: server, dataDir: dataDir, port: port,
	}
	service := "admin"
	if len(migrationService) > 0 {
		service = migrationService[0]
	}
	if err := RunMigrations(p.DSN, service); err != nil {
		_ = p.Close()
		return nil, err
	}
	return p, nil
}

func (p *Postgres) Close() error {
	var stopErr error
	if p.server != nil {
		stopErr = p.server.Stop()
	}
	removeErr := os.RemoveAll(p.dataDir)
	if stopErr != nil {
		return stopErr
	}
	return removeErr
}

var databaseNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func (p *Postgres) CreateDatabase(database, migrationService string) (string, error) {
	if !databaseNamePattern.MatchString(database) {
		return "", fmt.Errorf("invalid test database name %q", database)
	}
	db, err := sql.Open("postgres", p.DSN)
	if err != nil {
		return "", err
	}
	if _, err := db.Exec(`CREATE DATABASE ` + database); err != nil {
		_ = db.Close()
		return "", fmt.Errorf("create database %s: %w", database, err)
	}
	_ = db.Close()
	dsn := fmt.Sprintf("postgres://eagle:eagle@127.0.0.1:%d/%s?sslmode=disable", p.port, database)
	if err := RunMigrations(dsn, migrationService); err != nil {
		return "", err
	}
	return dsn, nil
}

func RunMigrations(dsn, service string) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("open migration database: %w", err)
	}
	defer func() { _ = db.Close() }()
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set migration dialect: %w", err)
	}
	goose.SetLogger(goose.NopLogger())
	root, err := RepoRoot()
	if err != nil {
		return err
	}
	if err := goose.Up(db, filepath.Join(root, "app", service, "migrations")); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}

func RepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.work not found")
		}
		dir = parent
	}
}

func availablePort() (uint32, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocate PostgreSQL test port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return 0, fmt.Errorf("release PostgreSQL test port: %w", err)
	}
	return uint32(port), nil
}
