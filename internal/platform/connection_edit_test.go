package platform_test

import (
	"datascope-server/internal/application"
	"datascope-server/internal/management"
	"datascope-server/internal/store"
	"errors"
	"testing"
)

func TestConnectionEditAuthorizationAndSQLite(t *testing.T) {
	f := setup(t)
	d, err := f.c.Get("default")
	must(t, err)
	d.Connection = &store.Connection{Host: "127.0.0.1", Port: 3306, Username: "reader"}
	in := application.Input{DatabaseID: d.ID, Database: d, Password: "must-not-be-saved"}
	for _, action := range []string{"edit_database_connection", "test_edited_database_connection"} {
		if _, err := f.app.Call(f.readerContext(), action, in); !errors.Is(err, management.ErrDenied) {
			t.Fatalf("%s allowed reader: %v", action, err)
		}
		if _, err := f.app.Call(f.ctx, action, in); err == nil {
			t.Fatalf("%s allowed SQLite target replacement", action)
		}
	}
	saved, err := f.c.Get(d.ID)
	must(t, err)
	if saved.Connection != nil || saved.Revision != d.Revision {
		t.Fatal("rejected edit changed source")
	}
	if _, err := f.m.DatabasePassword(f.ctx, d.ID); err == nil {
		t.Fatal("rejected edit stored password")
	}
}
