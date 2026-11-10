package catalog

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// ImportFile materializes a private copy before exposing it through normal source registration.
func (c *Catalog) ImportFile(ctx context.Context, name, format string, src io.Reader) (*Database, error) {
	if name == "" {
		return nil, errors.New("需要数据源名称")
	}
	if format != "csv" && format != "sqlite" {
		return nil, errors.New("仅支持 CSV 和 SQLite 文件")
	}
	root, err := c.managedSQLiteDirectory()
	if err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(root, "upload-*.sqlite")
	if err != nil {
		return nil, err
	}
	path := file.Name()
	keep := false
	defer func() {
		if !keep {
			os.Remove(path)
		}
	}()
	reader := bufio.NewReader(src)
	if format == "sqlite" {
		header, e := reader.Peek(16)
		if e != nil || !bytes.Equal(header, []byte("SQLite format 3\x00")) {
			file.Close()
			return nil, errors.New("无效的 SQLite 文件头")
		}
		_, err = io.Copy(file, reader)
	}
	closeErr := file.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if format == "csv" {
		err = importCSV(ctx, db, reader)
	} else {
		var check string
		err = db.QueryRowContext(ctx, "PRAGMA quick_check(1)").Scan(&check)
		if err == nil && check != "ok" {
			err = errors.New("SQLite 文件完整性检查失败")
		}
	}
	closeErr = db.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	created, err := c.Create(ctx, Database{Name: name, Driver: "sqlite", Path: path}, "")
	keep = err == nil
	return created, err
}

func importCSV(ctx context.Context, db *sql.DB, src *bufio.Reader) error {
	if bom, _ := src.Peek(3); bytes.Equal(bom, []byte{0xef, 0xbb, 0xbf}) {
		src.Discard(3)
	}
	reader := csv.NewReader(src)
	headers, err := reader.Read()
	if errors.Is(err, io.EOF) {
		return errors.New("CSV 文件缺少表头")
	}
	if err != nil {
		return err
	}
	cols := make([]string, len(headers))
	seen := map[string]bool{}
	for i, h := range headers {
		h = strings.TrimSpace(h)
		key := strings.ToLower(h)
		if h == "" || !utf8.ValidString(h) || strings.ContainsRune(h, 0) || seen[key] {
			return fmt.Errorf("CSV 第 %d 列的表头为空、重复或编码无效", i+1)
		}
		seen[key] = true
		cols[i] = `"` + strings.ReplaceAll(h, `"`, `""`) + `" TEXT`
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "CREATE TABLE data ("+strings.Join(cols, ",")+")"); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, "INSERT INTO data VALUES (?"+strings.Repeat(",?", len(cols)-1)+")")
	if err != nil {
		return err
	}
	defer stmt.Close()
	values := make([]any, len(cols))
	for {
		record, e := reader.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		for i, v := range record {
			if !utf8.ValidString(v) {
				line, _ := reader.FieldPos(i)
				return fmt.Errorf("CSV 第 %d 行不是有效的 UTF-8 文本", line)
			}
			values[i] = v
		}
		if _, e = stmt.ExecContext(ctx, values...); e != nil {
			return e
		}
	}
	return tx.Commit()
}
