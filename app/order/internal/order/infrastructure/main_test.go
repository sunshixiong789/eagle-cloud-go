package infrastructure

import (
	"flag"
	"fmt"
	"os"
	"testing"

	platformdb "github.com/eagle-go/eagle/app/order/internal/platform/database"
	"github.com/eagle-go/eagle/pkg/platform/config"
	"github.com/eagle-go/eagle/tests/testkit"
)

var testDB *platformdb.Database

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}
	pg, err := testkit.StartPostgres("order", "eagle_order_test", "order")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() { _ = pg.Close() }()
	db, cleanup, err := platformdb.Open(&config.Data{Database: &config.Data_Database{Dsn: pg.DSN, MaxConns: 4, MaxIdleConns: 1}})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	testDB = db
	defer cleanup()
	os.Exit(m.Run())
}
