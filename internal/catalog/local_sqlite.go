package catalog

import (
	"context"
	"database/sql"
	"datascope-server/internal/store"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
	if d == nil || d.Driver != "sqlite" || c.Management == nil {
		return false
	}
	root, err := filepath.EvalSymlinks(c.LocalSQLiteRoot())
	if err != nil {
		return false
	}
	// The managed directory itself must not redirect outside the configured location.
	if root != c.LocalSQLiteRoot() {
		return false
	}
	real, err := filepath.EvalSymlinks(d.Path)
	if err != nil {
		return false
	}
	if filepath.Dir(real) != root || real != d.Path {
		return false
	}
	info, err := os.Stat(real)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return c.Management.ValidatePath(real) == nil
}

func (c *Catalog) managedSQLiteDirectory() (string, error) {
	root := c.LocalSQLiteRoot()
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil || real != root {
		return "", errors.New("数据库目录不能包含符号链接")
	}
	return root, nil
}

func (c *Catalog) CreateLocalSQLite(ctx context.Context, d Database) (*Database, error) {
	if c.Management == nil || d.Driver != "sqlite" || d.Connection != nil || !sqliteFilename.MatchString(d.Path) {
		return nil, errors.New("新建 SQLite 需要文件名（字母、数字、下划线或连字符，以 .db/.sqlite/.sqlite3 结尾）")
	}
	root, err := c.managedSQLiteDirectory()
	if err != nil {
		return nil, err
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
	d, err := c.Get(id)
	if err != nil {
		return err
	}
	if !c.CanEditSchema(d) {
		return errors.New("仅允许修改项目 database 目录内的 SQLite 表结构")
	}
	if strings.TrimSpace(query) == "" {
		return errors.New("需要结构 SQL")
	}
	st, err := store.OpenWriter(ctx, d.Path, c.Timeout)
	if err != nil {
		return err
	}
	defer st.Close()
	return st.ExecuteSchema(ctx, query)
}
