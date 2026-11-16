#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Prepare fictional external event feeds and additive SQLite landing tables."""
import argparse
import datetime as dt
import json
import os
from pathlib import Path
import secrets
import sqlite3
import subprocess

ROOT = Path(__file__).resolve().parents[2]
CASES = [
    ('oracle', 'erp_customer_events', 'imported_erp_customers', 'Oracle 客户主数据增量', '客户信用额度（分）', 2, 11521),
    ('postgresql', 'warehouse_events', 'imported_warehouse_inventory', 'PostgreSQL 仓储库存增量', '可用库存件数', 3, 15432),
    ('mysql', 'online_order_events', 'imported_online_orders', 'MySQL 线上订单增量', '订单金额（分）', 5, 13316),
]

SCENARIOS = {
    'regional-sales': ('regional_sales_demo', '分地区销售', CASES),
    'academic': ('academic_demo', '高校教务', [
        ('oracle', 'student_registry_events', 'imported_student_registry', 'Oracle 学籍档案增量', '奖助学金额度（分）', 2, 11521),
        ('postgresql', 'attendance_summary_events', 'imported_attendance_summary', 'PostgreSQL 考勤汇总增量', '累计出勤课时', 3, 15432),
        ('mysql', 'course_signup_events', 'imported_course_signups', 'MySQL 在线选课增量', '选课学分', 5, 13316),
    ]),
    'channel-operations': ('channel_operations_demo', '渠道运营', [
        ('oracle', 'partner_account_events', 'imported_partner_accounts', 'Oracle 合作账号增量', '合作预算（分）', 2, 11521),
        ('postgresql', 'content_metric_events', 'imported_content_metrics', 'PostgreSQL 内容指标增量', '内容阅读或播放次数', 3, 15432),
        ('mysql', 'lead_conversion_events', 'imported_lead_conversions', 'MySQL 线索转化增量', '转化金额（分）', 5, 13316),
    ]),
    'securities': ('securities_demo', '证券业务', [
        ('oracle', 'customer_profile_events', 'imported_customer_profiles', 'Oracle 客户档案增量', '模拟资产金额（分）', 2, 11521),
        ('postgresql', 'market_quote_events', 'imported_market_quotes', 'PostgreSQL 行情快照增量', '模拟报价（分）', 3, 15432),
        ('mysql', 'account_application_events', 'imported_account_applications', 'MySQL 开户申请增量', '申请资料完成度（百分比）', 5, 13316),
    ]),
}
LABELS = {
    'student_registry_events': '虚构学生', 'attendance_summary_events': '虚构考勤', 'course_signup_events': '虚构选课',
    'partner_account_events': '虚构合作账号', 'content_metric_events': '虚构内容', 'lead_conversion_events': '虚构线索',
    'customer_profile_events': '虚构证券客户', 'market_quote_events': '虚构行情', 'account_application_events': '虚构开户申请',
}

SCRIPT = '''function main(params, context) {
  const source = sources.get("__DRIVER__-demo");
  const target = db.target();
  const last = Number(context.checkpoint.lastEventId || 0);
  const batch = Number(params.batchSize || 40);
  if (!Number.isSafeInteger(batch) || batch < 1 || batch > 500) throw new Error("batchSize must be 1..500");
  const rows = source.query("SELECT event_id, record_id, display_name, metric_value, is_deleted, occurred_at FROM __SOURCE__ WHERE event_id > ? AND occurred_at <= ? ORDER BY event_id LIMIT " + batch, [last, context.now]);
  function field(row, key) { return row[key] === undefined ? row[key.toUpperCase()] : row[key]; }
  let next = last;
  let inserted = 0, updated = 0;
  for (const row of rows) {
    const seq = Number(field(row, "event_id"));
    const id = Number(field(row, "record_id"));
    const metric = Number(field(row, "metric_value"));
    if (![seq, id, metric].every(Number.isSafeInteger)) throw new Error("unsafe integer in source");
    const label = String(field(row, "display_name")).trim();
    const deleted = Number(field(row, "is_deleted"));
    const old = target.query("SELECT source_seq FROM __TARGET__ WHERE id = ?", [id]);
    if (old.length === 0) {
      target.update("INSERT INTO __TARGET__(id, name, metric_value, source_seq, is_deleted, source_updated_at, imported_at) VALUES(?, ?, ?, ?, ?, ?, ?)", [id, label, metric, seq, deleted, field(row, "occurred_at"), context.now]);
      inserted++;
    } else if (Number(old[0].source_seq) < seq) {
      target.update("UPDATE __TARGET__ SET name = ?, metric_value = ?, source_seq = ?, is_deleted = ?, source_updated_at = ?, imported_at = ? WHERE id = ?", [label, metric, seq, deleted, field(row, "occurred_at"), context.now, id]);
      updated++;
    }
    next = seq;
  }
  context.saveCheckpoint({lastEventId: next, totalEvents: Number(context.checkpoint.totalEvents || 0) + rows.length});
  context.log("__TITLE__: read=" + rows.length + ", inserted=" + inserted + ", updated=" + updated + ", watermark=" + next + ", trial=" + context.trial);
  if (params.simulateFailure) throw new Error("演示主动失败：目标写入和进度应一起回滚");
}'''


def prepare(directory, target, database_id, events, cases=CASES, credentials_file=None):
    directory.mkdir(parents=True, exist_ok=True)
    os.chmod(directory, 0o700)
    env = credentials_file or directory / 'compose.env'
    env.parent.mkdir(parents=True, exist_ok=True)
    if not env.exists():
        env.write_text('DEMO_ADMIN_PASSWORD=A' + secrets.token_hex(14) + '\nDEMO_READER_PASSWORD=R' + secrets.token_hex(14) + '\n')
        os.chmod(env, 0o600)
    values = dict(line.split('=', 1) for line in env.read_text().splitlines() if line)
    password = values['DEMO_READER_PASSWORD']
    if not target.exists():
        raise SystemExit('Target SQLite must already exist')
    db = sqlite3.connect('file:' + str(target.resolve()) + '?mode=rw', uri=True)
    backup = directory / ('target-before-' + dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ') + '.sqlite3')
    with sqlite3.connect(backup) as out:
        db.backup(out)
    os.chmod(backup, 0o600)
    config = dict(version=1, database_id=database_id, connections=[], tasks=[])
    now = dt.datetime.now(dt.timezone.utc)
    for driver, source, landing, title, metric, minutes, port in cases:
        db.execute(f'CREATE TABLE IF NOT EXISTS {landing}(id INTEGER PRIMARY KEY, name TEXT NOT NULL, metric_value INTEGER NOT NULL, source_seq INTEGER NOT NULL, is_deleted INTEGER NOT NULL CHECK(is_deleted IN (0,1)), source_updated_at TEXT NOT NULL, imported_at TEXT NOT NULL)')
        oracle = driver == 'oracle'
        number, text = ('NUMBER(10)', 'VARCHAR2(150)') if oracle else ('INTEGER', 'VARCHAR(150)')
        ddl = f'CREATE TABLE {source}(event_id {number} PRIMARY KEY, record_id {number} NOT NULL, display_name {text} NOT NULL, metric_value {number} NOT NULL, is_deleted {number} NOT NULL, occurred_at VARCHAR(40) NOT NULL);'
        sql = [ddl]
        for seq in range(1, events + 1):
            record = (seq - 1) % 100 + 1
            label = LABELS.get(source, '虚构客户' if oracle else ('虚构仓位' if driver == 'postgresql' else '虚构网单')) + f'{record:03d}'
            value = record * 1000 + seq if driver != 'postgresql' else record * 2 + seq % 20
            if source == 'account_application_events': value = min(100, 30 + record % 70 + seq // 100)
            if source == 'course_signup_events': value = 1 + record % 5
            deleted = int(seq > 200 and seq % 37 == 0)
            # A finite, pre-generated event stream; future events become eligible over time.
            at = (now + dt.timedelta(seconds=(seq - 120) * 5)).strftime('%Y-%m-%dT%H:%M:%SZ')
            sql.append(f"INSERT INTO {source} VALUES({seq},{record},'{label}',{value},{deleted},'{at}');")
        sql.append('COMMIT;')
        if driver == 'mysql':
            sql += ["REVOKE ALL PRIVILEGES ON imports.* FROM 'import_reader'@'%';", "GRANT SELECT ON imports.* TO 'import_reader'@'%';"]
        if driver == 'postgresql':
            sql += [f"DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='import_reader') THEN CREATE USER import_reader WITH PASSWORD '{password}'; END IF; END $$;", 'GRANT CONNECT ON DATABASE imports TO import_reader;', 'GRANT USAGE ON SCHEMA public TO import_reader;', 'GRANT SELECT ON ALL TABLES IN SCHEMA public TO import_reader;']
        if oracle:
            sql.insert(0, 'WHENEVER SQLERROR EXIT SQL.SQLCODE ROLLBACK')
            sql.append('EXIT;')
        file = directory / (driver + '.sql')
        file.write_text('\n'.join(sql) + '\n', encoding='utf-8'); os.chmod(file, 0o600)
        connection = dict(host='127.0.0.1', port=port, username='IMPORT_READER' if oracle else 'import_reader', schema='IMPORTS' if oracle else ('public' if driver == 'postgresql' else 'imports'), allowed_schemas=['IMPORTS' if oracle else ('public' if driver == 'postgresql' else 'imports')])
        if oracle: connection['service_name'] = 'FREEPDB1'
        else: connection['database'] = 'imports'
        if driver in ('postgresql', 'mysql'): connection['tls_mode'] = 'disable'
        config['connections'].append(dict(id=driver+'-demo', name=title.replace('增量','来源（虚构数据）'), driver=driver, connection=connection, password_env='DEMO_READER_PASSWORD'))
        script = SCRIPT.replace('__DRIVER__', driver).replace('__SOURCE__', source).replace('__TARGET__', landing).replace('__TITLE__', title)
        if oracle:
            query = f"SELECT event_id, record_id, display_name, metric_value, is_deleted, occurred_at FROM {source} WHERE event_id > ? AND occurred_at <= ? ORDER BY event_id LIMIT "
            script = script.replace(query, query.upper())
        config['tasks'].append(dict(id='demo-'+driver+'-incremental', name=title, description=f'真实 {driver} 来源 → {landing}；每{minutes}分钟导入最多40个变更事件；metric_value 为{metric}。含新增、更新、软删除与幂等重放；全部为虚构数据。', script=script, sources=[driver+'-demo'], parameters=dict(batchSize=40, simulateFailure=False), schedule=dict(mode='interval', minutes=minutes, timezone='Asia/Shanghai'), enabled=True, timeout=60))
    db.commit(); db.close()
    manifest = directory / 'sources.json'
    manifest.write_text(json.dumps(config, ensure_ascii=False, indent=2) + '\n'); os.chmod(manifest, 0o600)
    print(f'Prepared {events} events per source, 3 landing tables; backup: {backup}; manifest: {manifest}')

def seed(directory, cases=CASES, credentials_file=None):
    """Explicitly seed the isolated Compose project, never other databases."""
    credentials_file = credentials_file or directory/'compose.env'
    values = dict(line.split('=', 1) for line in credentials_file.read_text().splitlines() if line)
    compose = ['docker', 'compose', '--env-file', str(credentials_file), '-f', str(Path(__file__).with_name('compose.yml'))]
    for driver, source, *_ in cases:
        sql = (directory/(driver+'.sql')).read_text()
        if driver == 'mysql':
            command = compose + ['exec', '-T', '-e', 'MYSQL_PWD='+values['DEMO_ADMIN_PASSWORD'], 'mysql', 'mysql', '--default-character-set=utf8mb4', '-uroot', 'imports']
        elif driver == 'postgresql':
            command = compose + ['exec', '-T', 'postgresql', 'psql', '-v', 'ON_ERROR_STOP=1', '-q', '-U', 'postgres', '-d', 'imports']
        else:
            sql = sql.replace('EXIT;','')
            sql = 'WHENEVER SQLERROR EXIT SQL.SQLCODE ROLLBACK\nALTER SESSION SET CONTAINER=FREEPDB1;\nALTER SESSION SET CURRENT_SCHEMA=IMPORTS;\n' + sql
            sql += "\nDECLARE n INTEGER; BEGIN SELECT COUNT(*) INTO n FROM DBA_USERS WHERE USERNAME='IMPORT_READER'; IF n=0 THEN EXECUTE IMMEDIATE 'CREATE USER IMPORT_READER IDENTIFIED BY " + values['DEMO_READER_PASSWORD'] + "'; END IF; END;\n/\nGRANT CREATE SESSION TO IMPORT_READER;\nGRANT SELECT ON IMPORTS." + source + " TO IMPORT_READER;\nEXIT;\n"
            command = compose + ['exec', '-T', 'oracle', 'sqlplus', '-s', '/', 'as', 'sysdba']
        result = subprocess.run(command, input=sql.encode(), stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        if result.returncode:
            # Do not expose SQL or command arguments containing local passwords.
            raise SystemExit(driver + ' seeding failed; check container health and whether the fixture already exists')
        print(driver + ': seeded fictional events and SELECT-only reader')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--scenario', choices=SCENARIOS, default='regional-sales')
    parser.add_argument('--directory', type=Path)
    parser.add_argument('--credentials-file', type=Path)
    parser.add_argument('--target', type=Path)
    parser.add_argument('--database-id')
    parser.add_argument('--seed', action='store_true', help='seed running dedicated Compose containers instead of preparing files')
    parser.add_argument('--events', type=int, default=720)
    args = parser.parse_args()
    if args.events < 120 or args.events > 10000: parser.error('--events must be 120..10000')
    database_id, _, cases = SCENARIOS[args.scenario]
    root = ROOT/'data/import-demo'
    directory = args.directory or (root if args.scenario == 'regional-sales' else root/args.scenario)
    credentials = args.credentials_file or (directory/'compose.env' if args.directory else root/'compose.env')
    if args.seed:
        seed(directory, cases, credentials)
    else:
        prepare(directory, args.target or ROOT/'data'/(database_id+'.sqlite3'), args.database_id or database_id, args.events, cases, credentials)
