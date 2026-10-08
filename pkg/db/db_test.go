package db_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"

	"github.com/eagle-go/eagle/pkg/db"
)

func TestConnectionPoolIdleLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	port := availablePort(t)
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Username("eagle").Password("eagle").Database("eagle_pool_test").Port(port).
		RuntimePath(filepath.Join(home, ".embedded-postgres-go", "eagle-pool")).
		DataPath(t.TempDir()).Logger(io.Discard))
	if err := pg.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.Stop() })
	dsn := fmt.Sprintf("postgres://eagle:eagle@127.0.0.1:%d/eagle_pool_test?sslmode=disable", port)
	for _, idleLimit := range []int32{0, 1, 2} {
		t.Run(fmt.Sprintf("idle=%d", idleLimit), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			pool, cleanup, err := db.New(ctx, db.Config{DSN: dsn, MaxConns: 2, MaxIdleConns: idleLimit})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(cleanup)
			first, err := pool.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = first.Close() })
			second, err := pool.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = second.Close() })
			if stats := pool.Stats(); stats.MaxOpenConnections != 2 || stats.InUse != 2 {
				t.Fatalf("pool with two checked-out connections: %+v", stats)
			}
			if err := first.Close(); err != nil {
				t.Fatal(err)
			}
			if err := second.Close(); err != nil {
				t.Fatal(err)
			}
			if stats := pool.Stats(); stats.InUse != 0 || stats.Idle != int(idleLimit) || stats.OpenConnections != int(idleLimit) {
				t.Fatalf("pool after returning connections, idle limit %d: %+v", idleLimit, stats)
			}
		})
	}
}
