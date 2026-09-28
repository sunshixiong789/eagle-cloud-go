package infrastructure

import (
	"flag"
	"fmt"
	"os"
	"testing"

	_ "github.com/eagle-go/eagle/api/eagle/dictionary/v1"
	_ "github.com/eagle-go/eagle/api/eagle/file/v1"
	_ "github.com/eagle-go/eagle/api/eagle/notification/v1"
	_ "github.com/eagle-go/eagle/api/eagle/order/v1"
	_ "github.com/eagle-go/eagle/api/eagle/product/v1"
	platformdb "github.com/eagle-go/eagle/app/admin/internal/platform/database"
	"github.com/eagle-go/eagle/pkg/platform/config"
	"github.com/eagle-go/eagle/tests/testkit"
)

var testDB *platformdb.Database

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

// Return before os.Exit so database and process cleanup always run.
func runTests(m *testing.M) int {
	flag.Parse()
	if testing.Short() {
		return m.Run()
	}
	pg, err := testkit.StartPostgres("access", "eagle_access_test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = pg.Close() }()
	db, cleanup, err := platformdb.Open(&config.Data{Database: &config.Data_Database{
		Dsn: pg.DSN, MaxConns: 4, MaxIdleConns: 1,
	}})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	testDB = db
	defer cleanup()
	return m.Run()
}
