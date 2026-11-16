#!/usr/bin/env python3
"""Create or explicitly expand a fictional shop: 200 customers, 100 products, 3000 orders."""
import argparse
from contextlib import closing
import datetime as dt
import json
import os
from pathlib import Path
import random
import sqlite3


def generate(path, expand_existing=False, seed=20261113):
    path = Path(path).resolve()
    path.parent.mkdir(parents=True, exist_ok=True)
    backup = None
    if expand_existing:
        db = sqlite3.connect(path.as_uri() + '?mode=rw', uri=True, timeout=30)
    else:
        fd = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        os.close(fd)
        db = sqlite3.connect(path)
    success = False
    try:
        db.execute('PRAGMA foreign_keys=ON')
        if not expand_existing:
            db.executescript(Path(__file__).with_name('schema.sqlite.sql').read_text(encoding='utf-8'))
        db.execute('BEGIN IMMEDIATE')
        if expand_existing:
            folder = path.parent / 'backups'
            folder.mkdir(parents=True, exist_ok=True)
            backup = folder / (path.stem + '-before-expand-' + dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ') + '.sqlite3')
            fd = os.open(backup, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
            os.close(fd)
            with closing(sqlite3.connect(path.as_uri() + '?mode=ro', uri=True)) as source, closing(sqlite3.connect(backup)) as saved:
                source.backup(saved)
                if saved.execute('PRAGMA integrity_check').fetchone()[0] != 'ok':
                    raise ValueError('Backup integrity validation failed')
        targets = {'customers': 200, 'products': 100, 'orders': 3000}
        existing = {t: {r[0] for r in db.execute(f'SELECT id FROM {t}')} for t in targets}
        for table, ids in existing.items():
            if any(i < 1 or i > targets[table] for i in ids):
                raise ValueError(f'{table} contains IDs outside the example range; expansion refused')
        for i in range(1, 201):
            if i not in existing['customers']:
                name = ['陈', '林', '王', '李', '张', '刘', '赵', '周', '徐', '孙'][(i-1)%10] + f'客{i:03d}'
                db.execute('INSERT INTO customers VALUES (?,?,?,?)', (i, name, f'customer{i}@example.invalid', (i-1)%4+1))
        for i in range(1, 101):
            if i not in existing['products']:
                price = 1000 + (i * 7919) % 49000
                name = ['笔记本', '中性笔', '水杯', '收纳袋', '桌面支架', '书签', '台灯', '鼠标垫', '文件夹', '便签'][(i-1)%10] + f' {i:03d}款'
                db.execute('INSERT INTO products VALUES (?,?,?,?,?,?)', (i, f'DEMO-{i:03d}', name, price, price*45//100, 0 if i%10 == 0 else 20+i%180))
        prices = dict(db.execute('SELECT id,price_cents FROM products'))
        start = dt.datetime(2026, 5, 1, 9, tzinfo=dt.timezone(dt.timedelta(hours=8)))
        # Per-order random sequences make reruns fill gaps without changing existing rows.
        for i in range(9, 3001):
            if i in existing['orders']:
                continue
            rng = random.Random(seed + i)
            when = start + dt.timedelta(days=(i-9)*184//2992, minutes=rng.randrange(480))
            cid = rng.randint(1, 190)  # Customers 191..200 intentionally have no orders.
            pid = rng.randint(1, 95)   # Products 96..100 intentionally have no sales.
            db.execute('INSERT INTO orders VALUES (?,?,?,?,?,?,?,?)',
                       (i, f'SHOP-{when:%Y%m%d}-{i:04d}', cid, pid, rng.randint(1, 8),
                        prices[pid], ['pending', 'paid', 'shipped', 'cancelled'][rng.randrange(4)], when.isoformat()))
        if db.execute('PRAGMA foreign_key_check').fetchone():
            raise ValueError('Foreign-key validation failed')
        if db.execute('PRAGMA integrity_check').fetchone()[0] != 'ok':
            raise ValueError('SQLite integrity validation failed')
        counts = {t: db.execute(f'SELECT COUNT(*) FROM {t}').fetchone()[0] for t in targets}
        if counts != targets:
            raise ValueError(f'Unexpected example counts: {counts}')
        db.commit()
        success = True
        return {'database': str(path), 'backup': str(backup) if backup else None, 'rows': counts,
                'bytes': path.stat().st_size}
    finally:
        db.close()
        if not success and not expand_existing:
            path.unlink(missing_ok=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--db', default='data/shop_demo.sqlite3')
    parser.add_argument('--expand-existing', action='store_true', help='Back up and append to an existing example; preserve all existing rows')
    parser.add_argument('--seed', type=int, default=20261113)
    args = parser.parse_args()
    print(json.dumps(generate(args.db, args.expand_existing, args.seed), ensure_ascii=False))
