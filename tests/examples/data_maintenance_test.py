import importlib.util
from contextlib import closing
from pathlib import Path
import sqlite3
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


def module(name, relative):
    spec = importlib.util.spec_from_file_location(name, ROOT / relative)
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


shop = module('shop_generator', 'examples/shop/generate.py')
securities = module('securities_compact', 'examples/securities/compact.py')
creator = module('sqlite_creator', 'examples/create_sqlite.py')


class MaintenanceTests(unittest.TestCase):
    def test_shop_determinism_boundaries_and_no_overwrite(self):
        with tempfile.TemporaryDirectory() as directory:
            paths = [Path(directory) / name for name in ('a.sqlite3', 'b.sqlite3')]
            for path in paths:
                shop.generate(path)
            self.assertEqual(paths[0].read_bytes(), paths[1].read_bytes())
            with self.assertRaises(FileExistsError):
                shop.generate(paths[0])
            with closing(sqlite3.connect(paths[0])) as db:
                self.assertEqual(db.execute('SELECT COUNT(DISTINCT substr(created_at,1,7)) FROM orders').fetchone()[0], 6)
                self.assertEqual(db.execute('SELECT COUNT(DISTINCT status) FROM orders').fetchone()[0], 4)
                self.assertEqual(db.execute('SELECT COUNT(DISTINCT owner_employee_id) FROM customers').fetchone()[0], 4)
                self.assertGreater(db.execute('SELECT COUNT(*) FROM customers c WHERE NOT EXISTS(SELECT 1 FROM orders o WHERE o.customer_id=c.id)').fetchone()[0], 0)
                self.assertGreater(db.execute('SELECT COUNT(*) FROM products p WHERE NOT EXISTS(SELECT 1 FROM orders o WHERE o.product_id=p.id)').fetchone()[0], 0)
                self.assertGreater(db.execute('SELECT COUNT(*) FROM products WHERE stock_quantity=0').fetchone()[0], 0)

    def test_shop_expansion_preserves_original_rows_and_backs_up(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'shop.sqlite3'
            creator.create(path, [ROOT / 'examples/shop/schema.sqlite.sql'])
            with closing(sqlite3.connect(path)) as db, db:
                original = {t: db.execute(f'SELECT * FROM {t} ORDER BY id').fetchall() for t in ('customers', 'products', 'orders')}
            result = shop.generate(path, expand_existing=True)
            with closing(sqlite3.connect(result['backup'])) as backup, closing(sqlite3.connect(path)) as db:
                for table, rows in original.items():
                    self.assertEqual(backup.execute(f'SELECT * FROM {table} ORDER BY id').fetchall(), rows)
                    self.assertEqual(db.execute(f'SELECT * FROM {table} ORDER BY id LIMIT ?', (len(rows),)).fetchall(), rows)
            before = path.read_bytes()
            shop.generate(path, expand_existing=True)
            self.assertEqual(path.read_bytes(), before)

    def test_compaction_backup_retention_and_untouched_ledgers(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'securities.sqlite3'
            with closing(sqlite3.connect(path)) as db, db:
                db.executescript('CREATE TABLE trading_calendar(trade_date TEXT,is_open INTEGER); CREATE TABLE positions(id INTEGER PRIMARY KEY,trade_date TEXT); CREATE TABLE daily_balances(id INTEGER PRIMARY KEY,trade_date TEXT); CREATE TABLE trades(id INTEGER PRIMARY KEY,amount INTEGER);')
                for day in range(1, 31):
                    date = f'2026-09-{day:02d}'
                    db.execute('INSERT INTO trading_calendar VALUES (?,1)', (date,))
                    for table in ('positions', 'daily_balances'):
                        db.execute(f'INSERT INTO {table} VALUES (?,?)', (day, date))
                    db.execute('INSERT INTO trades VALUES (?,?)', (day, day*100))
            before = path.read_bytes()
            with self.assertRaises(ValueError):
                securities.compact(path, 0)
            self.assertEqual(path.read_bytes(), before)
            result = securities.compact(path)
            with closing(sqlite3.connect(result['backup'])) as backup, closing(sqlite3.connect(path)) as db:
                for table in ('trades', 'trading_calendar'):
                    self.assertEqual(db.execute(f'SELECT * FROM {table}').fetchall(), backup.execute(f'SELECT * FROM {table}').fetchall())
                for table in ('positions', 'daily_balances'):
                    self.assertEqual(backup.execute(f'SELECT COUNT(*) FROM {table}').fetchone()[0], 30)
                    self.assertEqual(db.execute(f'SELECT * FROM {table} ORDER BY id').fetchall(), backup.execute(f'SELECT * FROM {table} WHERE trade_date>=? ORDER BY id', (result['from'],)).fetchall())
                    self.assertEqual(db.execute(f'SELECT COUNT(*) FROM {table}').fetchone()[0], 20)

    def test_ledger_window_preserves_regression_account_schema_and_values(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'securities.sqlite3'
            creator.create(path, [ROOT / 'examples/securities/schema.sqlite.sql', ROOT / 'examples/securities/seed.sql'])
            account = '880000001186'
            result = securities.compact(path, trim_ledgers=True, preserve_accounts=[account])
            self.assertEqual(result['ledger_checks'], 15)
            self.assertEqual(result['preserved_accounts'], [account])
            self.assertEqual(Path(result['backup']).stat().st_mode & 0o777, 0o600)
            with closing(sqlite3.connect(result['backup'])) as saved, closing(sqlite3.connect(path)) as db:
                schema = "SELECT type,name,sql FROM sqlite_master WHERE sql IS NOT NULL ORDER BY type,name"
                self.assertEqual(db.execute(schema).fetchall(), saved.execute(schema).fetchall())
                for (table,) in saved.execute("SELECT name FROM sqlite_master WHERE type='table'"):
                    suffix, params = '', ()
                    if table in ('orders', 'trades', 'cash_flows'):
                        suffix, params = ' WHERE trade_date>=? OR fund_account_no=?', (result['from'], account)
                        self.assertGreater(result['removed'][table], 0)
                    elif table in ('positions', 'daily_balances'):
                        suffix, params = ' WHERE trade_date>=?', (result['from'],)
                    self.assertEqual(db.execute(f'SELECT * FROM {table} ORDER BY rowid').fetchall(),
                                     saved.execute(f'SELECT * FROM {table}{suffix} ORDER BY rowid', params).fetchall(), table)
                self.assertTrue(db.execute('SELECT 1 FROM orders WHERE order_no=?', ('O0000002309',)).fetchone())
            again = securities.compact(path, trim_ledgers=True, preserve_accounts=[account])
            self.assertTrue(all(n == 0 for n in again['removed'].values()))

    def test_ledger_validation_failure_rolls_back_all_deletions(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'securities.sqlite3'
            creator.create(path, [ROOT / 'examples/securities/schema.sqlite.sql', ROOT / 'examples/securities/seed.sql'])
            with closing(sqlite3.connect(path)) as db, db:
                db.execute("UPDATE cash_flows SET customer_no=(SELECT customer_no FROM customers WHERE customer_no!=cash_flows.customer_no LIMIT 1) WHERE id=(SELECT MAX(id) FROM cash_flows)")
            before = path.read_bytes()
            with self.assertRaisesRegex(ValueError, 'Ledger validation failed'):
                securities.compact(path, trim_ledgers=True)
            self.assertEqual(path.read_bytes(), before)
            self.assertEqual(len(list((path.parent / 'backups').glob('*.sqlite3'))), 1)

    def test_invalid_preservation_options_cannot_modify_data(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'securities.sqlite3'
            creator.create(path, [ROOT / 'examples/securities/schema.sqlite.sql', ROOT / 'examples/securities/seed.sql'])
            before = path.read_bytes()
            for options in ({'preserve_accounts': ['880000001186']},
                            {'trim_ledgers': True, 'preserve_accounts': ["missing' OR 1=1 --"]}):
                with self.assertRaises(ValueError):
                    securities.compact(path, **options)
                self.assertEqual(path.read_bytes(), before)
