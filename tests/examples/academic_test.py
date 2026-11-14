import hashlib
import importlib.util
from pathlib import Path
import sqlite3
import tempfile
import unittest
from contextlib import closing
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('academic_generator', ROOT / 'examples/academic/generate.py')
academic = importlib.util.module_from_spec(spec)
spec.loader.exec_module(academic)


class AcademicDataTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.directory = tempfile.TemporaryDirectory()
        cls.path = Path(cls.directory.name) / 'academic.sqlite3'
        cls.result = academic.generate(cls.path)

    @classmethod
    def tearDownClass(cls):
        cls.directory.cleanup()

    def test_determinism_and_no_overwrite(self):
        other = Path(self.directory.name) / 'same.sqlite3'
        academic.generate(other)
        before = hashlib.sha256(self.path.read_bytes()).digest()
        self.assertEqual(before, hashlib.sha256(other.read_bytes()).digest())
        with self.assertRaises(FileExistsError):
            academic.generate(self.path)
        self.assertEqual(before, hashlib.sha256(self.path.read_bytes()).digest())

    def test_realistic_boundaries_and_snapshot(self):
        self.assertEqual(self.result['rows']['students'], 1000)
        self.assertEqual(self.result['rows']['teachers'], 80)
        self.assertEqual(self.result['invariant_checks'], 22)
        with closing(sqlite3.connect(self.path)) as db:
            checks = [
                "SELECT COUNT(*) FROM students WHERE status='leave'",
                "SELECT COUNT(*) FROM enrollments WHERE status='dropped'",
                "SELECT COUNT(*) FROM grades WHERE status='absent'",
                "SELECT COUNT(*) FROM grades WHERE status='pending'",
                "SELECT COUNT(*) FROM (SELECT section_id FROM section_teachers GROUP BY section_id HAVING COUNT(*)>1)",
                "SELECT COUNT(*) FROM sections WHERE status='cancelled'",
                "SELECT COUNT(*) FROM sections s WHERE status='open' AND (SELECT COUNT(*) FROM enrollments e WHERE e.section_id=s.id AND e.status='enrolled')=s.capacity",
                "SELECT COUNT(*) FROM sections s WHERE status='open' AND NOT EXISTS(SELECT 1 FROM enrollments e WHERE e.section_id=s.id)",
                "SELECT COUNT(*) FROM enrollments e JOIN sections s ON s.id=e.section_id WHERE s.term_id=2 AND e.status='enrolled' AND EXISTS(SELECT 1 FROM enrollments old JOIN sections os ON os.id=old.section_id WHERE old.student_id=e.student_id AND os.course_id=s.course_id AND os.term_id=1 AND old.status='completed')",
            ]
            for sql in checks:
                self.assertGreater(db.execute(sql).fetchone()[0], 0, sql)
            self.assertEqual(db.execute("SELECT COUNT(*) FROM attendance WHERE attendance_date>'2026-11-13'").fetchone()[0], 0)
            self.assertEqual(db.execute("SELECT COUNT(*) FROM grades WHERE graded_on>'2026-11-13'").fetchone()[0], 0)
            self.assertEqual(db.execute("SELECT COUNT(*) FROM grades g JOIN assessments a ON a.id=g.assessment_id JOIN sections s ON s.id=a.section_id WHERE s.term_id=2 AND a.kind='final' AND g.status<>'pending'").fetchone()[0], 0)
            self.assertEqual(db.execute("SELECT COUNT(*) FROM attendance WHERE attendance_date BETWEEN '2026-10-01' AND '2026-10-07'").fetchone()[0], 0)

    def test_corrupt_grade_relationship_rejected(self):
        with closing(sqlite3.connect(self.path)) as db:
            db.execute('BEGIN')
            db.execute('UPDATE grades SET assessment_id=(SELECT MAX(id) FROM assessments) WHERE id=1')
            with self.assertRaisesRegex(ValueError, 'grade_section'):
                academic.validate(db)
            db.rollback()

    def test_failed_generation_cleans_only_new_output(self):
        path = Path(self.directory.name) / 'failed.sqlite3'
        with patch.object(academic, 'validate', side_effect=ValueError('injected validation failure')):
            with self.assertRaises(ValueError):
                academic.generate(path)
        self.assertFalse(path.exists())
        with self.assertRaises(ValueError):
            academic.generate(path, as_of='2027-01-01')
        self.assertFalse(path.exists())
