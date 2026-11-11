#!/usr/bin/env python3
"""Create a SQLite database from ordered SQL files using Python's standard library."""
import argparse
import json
import os
from pathlib import Path
import sqlite3


def create(path, scripts):
    sql = '\n'.join(Path(script).read_text(encoding='utf-8') for script in scripts)
    target = Path(path)
    target.parent.mkdir(parents=True, exist_ok=True)
    descriptor = os.open(target, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    os.close(descriptor)
    connection = None
    complete = False
    try:
        connection = sqlite3.connect(target)
        connection.execute('PRAGMA foreign_keys=ON')
        connection.executescript('BEGIN;\n' + sql + '\nCOMMIT;')
        if connection.execute('PRAGMA foreign_key_check').fetchone():
            raise ValueError('Foreign-key validation failed')
        if connection.execute('PRAGMA integrity_check').fetchone()[0] != 'ok':
            raise ValueError('SQLite integrity validation failed')
        complete = True
        return {'database': str(target), 'tables': connection.execute(
            "SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%'"
        ).fetchone()[0]}
    finally:
        if connection is not None:
            connection.close()
        if not complete:
            target.unlink(missing_ok=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--db', required=True, help='Output file; existing files are never overwritten')
    parser.add_argument('scripts', nargs='+', help='SQL files, in execution order')
    args = parser.parse_args()
    print(json.dumps(create(args.db, args.scripts), ensure_ascii=False))
