from contextlib import closing
import hashlib
import importlib.util
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('channel_generator', ROOT / 'examples/channel-operations/generate.py')
generator = importlib.util.module_from_spec(spec)
spec.loader.exec_module(generator)


class ChannelOperationsTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.folder = tempfile.TemporaryDirectory()
        cls.path = Path(cls.folder.name) / 'operations.sqlite3'
        cls.result = generator.generate(cls.path)

    @classmethod
    def tearDownClass(cls):
        cls.folder.cleanup()

    def test_determinism_and_no_overwrite(self):
        other = Path(self.folder.name) / 'same.sqlite3'
        generator.generate(other)
        before = hashlib.sha256(self.path.read_bytes()).digest()
        self.assertEqual(before, hashlib.sha256(other.read_bytes()).digest())
        with self.assertRaises(FileExistsError):
            generator.generate(self.path)
        self.assertEqual(before, hashlib.sha256(self.path.read_bytes()).digest())

    def test_missing_corrections_attribution_and_lifecycle(self):
        self.assertEqual(self.result['rows']['publications'], 1600)
        self.assertEqual(self.result['rows']['content_items'], 600)
        self.assertEqual(self.result['invariant_checks'], 22)
        with closing(sqlite3.connect(self.path)) as db:
            positives = [
                "SELECT COUNT(*) FROM publications WHERE status='failed'",
                "SELECT COUNT(*) FROM publications WHERE status='scheduled'",
                "SELECT COUNT(*) FROM publications WHERE status='removed'",
                "SELECT COUNT(*) FROM publication_daily_metrics WHERE availability='missing' AND views IS NULL",
                "SELECT COUNT(*) FROM publication_daily_metrics WHERE availability='complete' AND organic_views IS NULL",
                "SELECT COUNT(*) FROM publication_daily_metrics WHERE revision=2",
                "SELECT COUNT(*) FROM (SELECT cumulative_views,LAG(cumulative_views) OVER(PARTITION BY publication_id ORDER BY captured_at,revision) previous FROM publication_metric_snapshots) WHERE cumulative_views<previous",
                "SELECT COUNT(*) FROM conversion_events WHERE attribution='unknown' AND link_id IS NULL",
                "SELECT COUNT(*) FROM conversion_events WHERE quality='duplicate'",
            ]
            for query in positives:
                self.assertGreater(db.execute(query).fetchone()[0], 0, query)
            for table, column in [('comments','created_at'),('conversion_events','occurred_at'),('publication_metric_snapshots','captured_at'),('publication_events','occurred_at')]:
                self.assertEqual(db.execute(f"SELECT COUNT(*) FROM {table} WHERE substr({column},1,10)>'2026-11-13'").fetchone()[0], 0, table)
            self.assertEqual(db.execute("SELECT COUNT(*) FROM publication_daily_metrics WHERE organic_views IS NOT NULL AND views<>organic_views+paid_views").fetchone()[0], 0)

    def test_invalid_cross_campaign_link_rejected(self):
        with closing(sqlite3.connect(self.path)) as db:
            db.execute('BEGIN')
            db.execute('UPDATE tracked_links SET campaign_id=CASE WHEN campaign_id=1 THEN 2 ELSE 1 END WHERE id=(SELECT MIN(id) FROM tracked_links)')
            with self.assertRaisesRegex(ValueError, 'link_campaign'):
                generator.validate(db)
            db.rollback()

    def test_failure_cleans_partial_file(self):
        path = Path(self.folder.name) / 'failed.sqlite3'
        with patch.object(generator, 'validate', side_effect=ValueError('injected failure')):
            with self.assertRaises(ValueError):
                generator.generate(path)
        self.assertFalse(path.exists())
