package regionalsales

import (
	"context"
	"datascope-server/internal/application"
	"datascope-server/internal/catalog"
	"datascope-server/internal/configbundle"
	"datascope-server/internal/rewrite"
	"datascope-server/internal/store"
	"path/filepath"
	"runtime"
)

func asset(name string) string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "../../../examples/regional-sales", name)
}
func Examples() []catalog.SQLExample {
	b, err := configbundle.Load(asset("bundle.yml"))
	if err != nil {
		panic(err)
	}
	return b.Database.SQLExamples
}
func Policies(context.Context, *store.Store) (*rewrite.Config, error) {
	return rewrite.LoadConfig(asset("rewrite.yml"))
}
func Install(ctx context.Context, app *application.Service, path, password string) (*catalog.Database, error) {
	b, err := configbundle.Load(asset("bundle.yml"))
	if err != nil {
		return nil, err
	}
	b.Database.Path = path
	if _, err = configbundle.Import(ctx, app, b, password, false); err != nil {
		return nil, err
	}
	return app.Catalog.Get(b.Database.ID)
}
