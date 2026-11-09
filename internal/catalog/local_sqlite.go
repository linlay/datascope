package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
)

var sqliteFilename = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}\.(db|sqlite|sqlite3)$`)

// LocalSQLiteRoot keeps business files separate from private management state.
func (c *Catalog) LocalSQLiteRoot() string {
	if c.Management == nil {
		return ""
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(c.Management.Path))
	if err != nil {
		return ""
	}
	root, _ := filepath.Abs(filepath.Join(parent, "database"))
	return root
}

func (c *Catalog) CanEditSchema(d *Database) bool {
	if d == nil || d.Driver != "sqlite" || c.Management == nil || d.DDLEnabled == nil || !*d.DDLEnabled {
		return false
	}
	real, err := filepath.EvalSymlinks(d.Path)
	if err != nil {
		return false
	}
	info, err := os.Stat(real)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return c.Management.ValidatePath(real) == nil
}

func (c *Catalog) CreateLocalSQLite(ctx context.Context, d Database) (*Database, error) {
	if c.Management == nil || d.Driver != "sqlite" || d.Connection != nil || !sqliteFilename.MatchString(d.Path) {
		return nil, errors.New("新建 SQLite 需要文件名（字母、数字、下划线或连字符，以 .db/.sqlite/.sqlite3 结尾）")
	}
	root := c.LocalSQLiteRoot()
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil || real != root {
		return nil, errors.New("数据库目录不能包含符号链接")
	}
	d.Path = filepath.Join(root, d.Path)
	f, err := os.OpenFile(d.Path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("创建数据库文件失败（文件可能已存在）: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			os.Remove(d.Path)
		}
	}()
	if err = f.Close(); err != nil {
		return nil, err
	}
	if err = c.Management.ValidatePath(d.Path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: d.Path, RawQuery: "mode=rw"}).String())
	if err != nil {
		return nil, err
	}
	// Materialize a valid SQLite header even for an empty business database.
	_, err = db.ExecContext(ctx, "VACUUM")
	closeErr := db.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	created, err := c.Create(ctx, d, "")
	if err == nil {
		keep = true
	}
	return created, err
}

func (c *Catalog) EditSchema(ctx context.Context, id, query string) error {
	return errors.New("请使用 preview_ddl 和 apply_ddl 预览后执行结构变更")
}
