#!/usr/bin/env python3
"""Back up securities data, retain N snapshot days and optionally trim ledgers."""
import argparse
from contextlib import closing
import datetime as dt
import json
import os
from pathlib import Path
import sqlite3


# Keep the same ledger invariants as the full-data regression generator.
LEDGER_CHECKS = (
    ('foreign keys', 'SELECT count(*) FROM pragma_foreign_key_check'),
    ('cash-flow ownership and trade links', 'SELECT count(*) FROM cash_flows f JOIN fund_accounts a ON a.fund_account_no=f.fund_account_no LEFT JOIN trades t ON t.id=f.trade_id WHERE a.customer_no!=f.customer_no OR (f.trade_id IS NOT NULL AND (t.customer_no!=f.customer_no OR t.fund_account_no!=f.fund_account_no OR t.trade_date!=f.trade_date))'),
    ('position ownership', 'SELECT count(*) FROM positions p JOIN fund_accounts a ON a.fund_account_no=p.fund_account_no WHERE a.customer_no!=p.customer_no'),
    ('balance ownership', 'SELECT count(*) FROM daily_balances b JOIN fund_accounts a ON a.fund_account_no=b.fund_account_no WHERE a.customer_no!=b.customer_no'),
    ('cash-flow continuity', 'SELECT count(*) FROM (SELECT fund_account_no,trade_date,amount_cents,balance_after,LAG(balance_after) OVER(PARTITION BY fund_account_no ORDER BY id) AS prev FROM cash_flows) f JOIN daily_balances b ON b.fund_account_no=f.fund_account_no AND b.trade_date=f.trade_date WHERE f.balance_after!=COALESCE(f.prev,b.opening_balance)+f.amount_cents'),
    ('position trade quantities', "SELECT count(*) FROM positions p WHERE p.buy_quantity!=COALESCE((SELECT sum(t.quantity) FROM trades t WHERE t.fund_account_no=p.fund_account_no AND t.security_code=p.security_code AND t.trade_date=p.trade_date AND t.side='BUY'),0) OR p.sell_quantity!=COALESCE((SELECT sum(t.quantity) FROM trades t WHERE t.fund_account_no=p.fund_account_no AND t.security_code=p.security_code AND t.trade_date=p.trade_date AND t.side='SELL'),0)"),
    ('order account and market rules', "SELECT count(*) FROM orders o JOIN fund_accounts f ON f.fund_account_no=o.fund_account_no JOIN stock_accounts a ON a.id=o.stock_account_id JOIN securities s ON s.code=o.security_code WHERE f.customer_no!=o.customer_no OR a.fund_account_no!=o.fund_account_no OR a.exchange!=s.exchange OR (s.board='STAR' AND o.side='BUY' AND o.quantity<200)"),
    ('order commissions', 'SELECT count(*) FROM orders o WHERE filled_quantity>0 AND (SELECT sum(commission_cents) FROM trades t WHERE t.order_id=o.id)!=max(500,(o.price*o.filled_quantity*25+50000)/100000)'),
    ('trade order links', 'SELECT count(*) FROM trades t JOIN orders o ON o.id=t.order_id WHERE t.customer_no!=o.customer_no OR t.fund_account_no!=o.fund_account_no OR t.security_code!=o.security_code OR t.side!=o.side OR t.trade_date!=o.trade_date'),
    ('filled order quantities', 'SELECT count(*) FROM orders o WHERE o.filled_quantity != COALESCE((SELECT SUM(t.quantity) FROM trades t WHERE t.order_id=o.id),0)'),
    ('order prices and lots', 'SELECT count(*) FROM orders o JOIN daily_quotes q ON q.security_code=o.security_code AND q.trade_date=o.trade_date WHERE o.price<q.low OR o.price>q.high OR o.quantity%100!=0'),
    ('sell availability', 'SELECT count(*) FROM positions WHERE sell_quantity>opening_quantity OR available_quantity!=quantity-buy_quantity'),
    ('position continuity', 'SELECT count(*) FROM (SELECT opening_quantity,lag(quantity) OVER(PARTITION BY fund_account_no,security_code ORDER BY trade_date) p FROM positions) WHERE p IS NOT NULL AND opening_quantity!=p'),
    ('balance continuity', 'SELECT count(*) FROM (SELECT opening_balance,lag(closing_balance) OVER(PARTITION BY fund_account_no ORDER BY trade_date) p FROM daily_balances) WHERE p IS NOT NULL AND opening_balance!=p'),
    ('daily trade amounts and fees', "SELECT count(*) FROM daily_balances b WHERE b.buy_amount!=COALESCE((SELECT sum(amount_cents) FROM trades t WHERE t.fund_account_no=b.fund_account_no AND t.trade_date=b.trade_date AND t.side='BUY'),0) OR b.sell_amount!=COALESCE((SELECT sum(amount_cents) FROM trades t WHERE t.fund_account_no=b.fund_account_no AND t.trade_date=b.trade_date AND t.side='SELL'),0) OR b.fees!=COALESCE((SELECT sum(fee_cents) FROM trades t WHERE t.fund_account_no=b.fund_account_no AND t.trade_date=b.trade_date),0)"),
)


def validate_ledgers(db):
    for name, query in LEDGER_CHECKS:
        violations = db.execute(query).fetchone()[0]
        if violations:
            raise ValueError(f'Ledger validation failed ({name}): {violations} rows; transaction rolled back')
    return len(LEDGER_CHECKS)


def compact(path, keep_days=20, backup_dir=None, *, trim_ledgers=False, preserve_accounts=()):
    if keep_days < 1:
        raise ValueError('keep-days must be positive')
    preserve_accounts = sorted(set(preserve_accounts))
    if preserve_accounts and not trim_ledgers:
        raise ValueError('preserve-account requires trim-ledgers')
    path = Path(path).resolve()
    backup_dir = Path(backup_dir) if backup_dir else path.parent / 'backups'
    backup_dir.mkdir(parents=True, exist_ok=True)
    backup = backup_dir / (path.stem + '-before-compact-' + dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ') + '.sqlite3')
    db = sqlite3.connect(path.as_uri() + '?mode=rw', uri=True, timeout=30)
    try:
        db.execute('PRAGMA foreign_keys=ON')
        # Reserve the writer while another connection takes a consistent backup.
        db.execute('BEGIN IMMEDIATE')
        for account in preserve_accounts:
            if not db.execute('SELECT 1 FROM fund_accounts WHERE fund_account_no=?', (account,)).fetchone():
                raise ValueError(f'Unknown preserved account: {account}')
        fd = os.open(backup, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        os.close(fd)
        with closing(sqlite3.connect(path.as_uri() + '?mode=ro', uri=True)) as source, closing(sqlite3.connect(backup)) as saved:
            source.backup(saved)
            if saved.execute('PRAGMA integrity_check').fetchone()[0] != 'ok':
                raise ValueError('Backup integrity validation failed')
        before_bytes = path.stat().st_size
        dates = [r[0] for r in db.execute('SELECT DISTINCT trade_date FROM daily_balances ORDER BY trade_date DESC LIMIT ?', (keep_days,))]
        if not dates:
            raise ValueError('No daily balance snapshots')
        if any(not db.execute('SELECT 1 FROM trading_calendar WHERE trade_date=? AND is_open=1', (day,)).fetchone() for day in dates):
            raise ValueError('Snapshot date is not an open trading day')
        cutoff = min(dates)
        removed = {}
        if trim_ledgers:
            # An unindexed child FK otherwise makes bulk trade deletion quadratic.
            # Drop only the helper index created here; retain all original indexes.
            helper = 'compact_cash_flows_trade_lookup'
            create_helper = not db.execute('SELECT 1 FROM sqlite_master WHERE name=?', (helper,)).fetchone()
            if create_helper:
                db.execute(f'CREATE INDEX {helper} ON cash_flows(trade_id)')
            condition = 'trade_date < ?'
            parameters = [cutoff]
            if preserve_accounts:
                condition += ' AND fund_account_no NOT IN (' + ','.join('?' for _ in preserve_accounts) + ')'
                parameters.extend(preserve_accounts)
            for table in ('cash_flows', 'trades', 'orders'):
                removed[table] = db.execute(f'DELETE FROM {table} WHERE {condition}', parameters).rowcount
            if create_helper:
                db.execute(f'DROP INDEX {helper}')
        for table in ('positions', 'daily_balances'):
            removed[table] = db.execute(f'DELETE FROM {table} WHERE trade_date < ?', (cutoff,)).rowcount
        if db.execute('PRAGMA foreign_key_check').fetchone():
            raise ValueError('Foreign-key validation failed; transaction rolled back')
        ledger_checks = validate_ledgers(db) if trim_ledgers else None
        db.commit()
        # Keep the same database file so existing registered connections stay valid.
        db.execute('VACUUM')
        if db.execute('PRAGMA integrity_check').fetchone()[0] != 'ok':
            raise ValueError(f'Integrity validation failed; restore backup {backup}')
        return {'database': str(path), 'backup': str(backup), 'from': cutoff,
                'through': max(dates), 'trading_days': len(dates), 'removed': removed,
                'trim_ledgers': trim_ledgers, 'preserved_accounts': preserve_accounts,
                'ledger_checks': ledger_checks,
                'before_bytes': before_bytes, 'after_bytes': path.stat().st_size}
    finally:
        db.close()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--db', default='data/securities_demo.sqlite3')
    parser.add_argument('--keep-days', type=int, default=20)
    parser.add_argument('--backup-dir')
    parser.add_argument('--trim-ledgers', action='store_true', help='Also trim orders, trades and cash flows to the snapshot window')
    parser.add_argument('--preserve-account', action='append', default=[], help='Retain this fund account\'s complete ledgers (repeatable; requires --trim-ledgers)')
    args = parser.parse_args()
    print(json.dumps(compact(args.db, args.keep_days, args.backup_dir,
                             trim_ledgers=args.trim_ledgers, preserve_accounts=args.preserve_account), ensure_ascii=False))
