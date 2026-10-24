// Package testutil creates an isolated demo database; tests never require tracked binary data.
package testutil

import (
	"os"
	"path/filepath"
	"runtime"
	"datascope-server/internal/gendata"
	"sync"
	"testing"
)

var once sync.Once
var path string
var failure error

func Demo(t testing.TB) string {
	t.Helper()
	once.Do(func() {
		var d string
		d, failure = os.MkdirTemp("", "securities-test-")
		if failure != nil {
			return
		}
		_, file, _, _ := runtime.Caller(0)
		path = filepath.Join(d, "demo.sqlite3")
		failure = gendata.Generate(gendata.Options{Path: path, Schema: filepath.Join(filepath.Dir(file), "../../sql/schema.sqlite.sql")})
	})
	if failure != nil {
		t.Fatal(failure)
	}
	return path
}
