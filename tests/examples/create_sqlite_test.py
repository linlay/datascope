import importlib.util
from pathlib import Path
import sqlite3
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('create_sqlite', ROOT / 'examples/create_sqlite.py')
creator = importlib.util.module_from_spec(spec)
spec.loader.exec_module(creator)


class CreateSQLiteTests(unittest.TestCase):
    def test_shop_seed_and_existing_file_preserved(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'shop.sqlite3'
            result = creator.create(path, [ROOT / 'examples/shop/schema.sqlite.sql'])
            self.assertEqual(result['tables'], 3)
            with sqlite3.connect(path) as db:
                self.assertEqual(db.execute('SELECT COUNT(*) FROM orders').fetchone()[0], 8)
            before = path.read_bytes()
            with self.assertRaises(FileExistsError):
                creator.create(path, [ROOT / 'examples/shop/schema.sqlite.sql'])
            self.assertEqual(before, path.read_bytes())

    def test_failed_sql_removes_only_new_output(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'failed.sqlite3'
            sql = Path(directory) / 'bad.sql'
            sql.write_text('CREATE TABLE x(id INTEGER); INSERT INTO missing VALUES (1);')
            with self.assertRaises(sqlite3.Error):
                creator.create(path, [sql])
            self.assertFalse(path.exists())
