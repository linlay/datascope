// Command gen-shop creates a small, fictional shop database without overwriting files.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	path := flag.String("db", "data/shop_demo.sqlite3", "输出 SQLite 文件（不能已存在）")
	schema := flag.String("schema", "sql/shop_demo.sqlite.sql", "建表与样例数据 SQL")
	flag.Parse()
	data, err := os.ReadFile(*schema)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(*path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(*path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	f.Close()
	keep := false
	defer func() {
		if !keep {
			os.Remove(*path)
		}
	}()
	db, err := sql.Open("sqlite", *path)
	if err != nil {
		return err
	}
	defer db.Close()
	// SQLite foreign_keys must be set before starting a transaction.
	db.SetMaxOpenConns(1)
	if _, err = db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(string(data)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	keep = true
	fmt.Println("Created", *path, "(4 customers, 6 products, 8 orders)")
	return nil
}
