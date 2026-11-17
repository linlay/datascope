#!/usr/bin/env python3
"""Create two tiny related sources and an import manifest; never write the target."""
import argparse
import datetime as dt
import json
import os
from pathlib import Path
import sqlite3

ROOT = Path(__file__).resolve().parents[2]
BASE = 8100000
ORDER_COUNT = 20
TABLES = {
    'orders': ['sales_orders', 'sales_order_items'],
    'fulfillment': ['shipments', 'shipment_items', 'sales_returns', 'sales_return_items'],
    'settlements': ['payments', 'payment_allocations', 'refunds'],
    'inventory': ['inventory_balances'],
}


def connect(path, mode='ro'):
    db = sqlite3.connect(path.resolve().as_uri() + '?mode=' + mode, uri=True)
    db.row_factory = sqlite3.Row
    db.execute('PRAGMA foreign_keys=ON')
    return db


def insert(db, table, row):
    db.execute(f"INSERT INTO {table}({','.join(row)}) VALUES({','.join('?' for _ in row)})", tuple(row.values()))


def rows(db, sql, args=()):
    return [dict(r) for r in db.execute(sql, args)]


def columns(db, table):
    return [r['name'] for r in db.execute(f'PRAGMA table_info({table})')]


def copy_row(db, table, row):
    insert(db, table, {k: row[k] for k in columns(db, table)})


def put_function(db, table):
    cols = columns(db, table)
    # SQL is generated once into the saved script, not interpolated from runtime input.
    return f'''function put_{table}(r) {{
    const values = [{', '.join('r.' + c for c in cols)}];
    const old = target.query("SELECT {', '.join(cols)} FROM {table} WHERE id = ?", [r.id]);
    if (old.length === 0) target.update("INSERT INTO {table}({','.join(cols)}) VALUES({','.join('?' for _ in cols)})", values);
    else {{
      if ({' || '.join('old[0].' + c + ' !== r.' + c for c in cols)})
        target.update("UPDATE {table} SET {','.join(c + ' = ?' for c in cols)} WHERE id = ?", values.concat([r.id]));
    }}
  }}'''


def make_script(db, kind):
    alias = 'sales-transactions' if kind in ('orders', 'settlements') else 'warehouse-fulfillment'
    change_table = 'settlement_changes' if kind == 'settlements' else 'stock_changes' if kind == 'inventory' else 'order_changes'
    key = 'stock_id' if kind == 'inventory' else 'order_id'
    functions = '\n  '.join(put_function(db, t) for t in TABLES[kind])
    bodies = {
        'orders': '''
      const order = source.query("SELECT * FROM sales_orders WHERE id = ?", [id])[0];
      const customer = source.query("SELECT customer_no FROM customers WHERE id = ?", [order.customer_id])[0];
      const existingCustomer = target.query("SELECT customer_no FROM customers WHERE id = ?", [order.customer_id]);
      if (!existingCustomer.length || existingCustomer[0].customer_no !== customer.customer_no) throw new Error("Customer mapping mismatch");
      const oldOrder = target.query("SELECT order_no FROM sales_orders WHERE id = ?", [id]);
      if (oldOrder.length && oldOrder[0].order_no !== order.order_no) throw new Error("Order ID collision");
      put_sales_orders(order);
      for (const line of source.query("SELECT * FROM sales_order_items WHERE order_id = ? ORDER BY id LIMIT 100", [id])) {
        const product = source.query("SELECT sku FROM products WHERE id = ?", [line.product_id])[0];
        const existingProduct = target.query("SELECT sku FROM products WHERE id = ?", [line.product_id]);
        if (!existingProduct.length || existingProduct[0].sku !== product.sku) throw new Error("Product mapping mismatch");
        put_sales_order_items(line);
      }''',
        'fulfillment': '''
      requireOrder(id);
      for (const shipment of source.query("SELECT * FROM shipments WHERE order_id = ? ORDER BY id LIMIT 100", [id])) {
        const w = source.query("SELECT name FROM warehouses WHERE id = ?", [shipment.warehouse_id])[0];
        const mapped = target.query("SELECT name FROM warehouses WHERE id = ?", [shipment.warehouse_id]);
        if (!mapped.length || mapped[0].name !== w.name) throw new Error("Warehouse mapping mismatch");
        put_shipments(shipment);
        for (const line of source.query("SELECT * FROM shipment_items WHERE shipment_id = ? ORDER BY id LIMIT 100", [shipment.id])) {
          requireLine(line.order_item_id, id);
          put_shipment_items(line);
        }
      }
      for (const ret of source.query("SELECT * FROM sales_returns WHERE order_id = ? ORDER BY id LIMIT 100", [id])) {
        put_sales_returns(ret);
        for (const line of source.query("SELECT * FROM sales_return_items WHERE return_id = ? ORDER BY id LIMIT 100", [ret.id])) {
          requireLine(line.order_item_id, id);
          put_sales_return_items(line);
        }
      }''',
        'settlements': '''
      requireOrder(id);
      for (const allocation of source.query("SELECT * FROM payment_allocations WHERE order_id = ? ORDER BY id LIMIT 100", [id])) {
        put_payments(source.query("SELECT * FROM payments WHERE id = ?", [allocation.payment_id])[0]);
        put_payment_allocations(allocation);
        for (const refund of source.query("SELECT * FROM refunds WHERE payment_id = ? ORDER BY id LIMIT 100", [allocation.payment_id])) {
          if (!target.query("SELECT id FROM sales_returns WHERE id = ? AND order_id = ?", [refund.return_id, id]).length)
            throw new Error("Return dependency missing; run fulfillment first");
          put_refunds(refund);
        }
      }''',
        'inventory': '''
      const stock = source.query("SELECT * FROM inventory_balances WHERE id = ?", [id])[0];
      const w = source.query("SELECT name FROM warehouses WHERE id = ?", [stock.warehouse_id])[0];
      const mapped = target.query("SELECT name FROM warehouses WHERE id = ?", [stock.warehouse_id]);
      if (!mapped.length || mapped[0].name !== w.name) throw new Error("Warehouse mapping mismatch");
      if (!target.query("SELECT id FROM products WHERE id = ?", [stock.product_id]).length) throw new Error("Product dependency missing");
      put_inventory_balances(stock);''',
    }
    require_order = '''
  function requireOrder(id) {
    const expected = source.query("SELECT order_no FROM __ORDERS__ WHERE id = ?", [id])[0];
    const actual = target.query("SELECT order_no FROM sales_orders WHERE id = ?", [id]);
    if (!actual.length || actual[0].order_no !== expected.order_no) throw new Error("Order dependency missing; run orders first");
  }
  function requireLine(id, orderId) {
    if (!target.query("SELECT id FROM sales_order_items WHERE id = ? AND order_id = ?", [id, orderId]).length)
      throw new Error("Order line dependency missing; run orders first");
  }'''.replace('__ORDERS__', 'sales_orders' if kind == 'settlements' else 'order_references')
    return f'''function main(params, context) {{
  const source = sources.get("{alias}");
  const target = db.target();
  const batch = params.batchSize;
  if (!Number.isSafeInteger(batch) || batch < 1 || batch > 20) throw new Error("batchSize must be 1..20");
  {functions}
  {require_order if kind in ('fulfillment', 'settlements') else ''}
  let last = Number(context.checkpoint.lastSeq || 0);
  const changes = source.query("SELECT {key}, change_seq FROM {change_table} WHERE change_seq > ? ORDER BY change_seq LIMIT " + batch, [last]);
  for (const change of changes) {{
    const id = change.{key};
    if (!Number.isSafeInteger(id) || id <= {BASE} || id >= {BASE + 10000}) throw new Error("Outside demo ID range");
    {bodies[kind]}
    last = change.change_seq;
  }}
  context.saveCheckpoint({{lastSeq: last}});
  context.log("{kind}: objects=" + changes.length + ", watermark=" + last);
  if (params.simulateFailure) throw new Error("Simulated failure; writes and checkpoint must roll back");
}}
'''


def prepare(target, directory, database_id):
    directory = directory.resolve()
    trading = directory / 'sales_transactions_demo.sqlite3'
    warehouse = directory / 'warehouse_fulfillment_demo.sqlite3'
    outputs = [trading, warehouse, directory / 'sources.json']
    if any(p.exists() for p in outputs):
        raise ValueError('Demo already exists; refusing to overwrite sources or reset checkpoints')
    directory.mkdir(parents=True, exist_ok=True)
    made = []
    source = oms = wms = None
    try:
        source = connect(target)
        source.execute('BEGIN')  # All reference mappings come from one target snapshot.
        warehouses = rows(source, 'SELECT * FROM warehouses ORDER BY id LIMIT 2')
        if len(warehouses) != 2:
            raise ValueError('Target needs two warehouses')
        customers = rows(source, 'SELECT * FROM customers WHERE office_id=? ORDER BY id LIMIT 3', (warehouses[0]['office_id'],))
        customers += rows(source, 'SELECT * FROM customers WHERE office_id=? AND id NOT IN (' + ','.join('?' for _ in customers) + ') ORDER BY id LIMIT 2', (warehouses[1]['office_id'], *(c['id'] for c in customers)))
        products = rows(source, 'SELECT * FROM products WHERE active=1 ORDER BY id LIMIT 6')
        if len(customers) != 5 or len(products) != 6:
            raise ValueError('Target needs five customers and six active products')
        for table in sum(TABLES.values(), []):
            if source.execute(f'SELECT 1 FROM {table} WHERE id>? AND id<? LIMIT 1', (BASE, BASE + 10000)).fetchone():
                raise ValueError('Reserved demo ID range already used in target: ' + table)
        for path, name in [(trading, 'sales-transactions'), (warehouse, 'warehouse-fulfillment')]:
            fd = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
            os.close(fd)
            made.append(path)
            db = connect(path, 'rw')
            if name == 'sales-transactions':
                oms = db
            else:
                wms = db
            db.executescript((ROOT / 'examples' / name / 'schema.sqlite.sql').read_text())
        for c in customers:
            copy_row(oms, 'customers', c)
        for p in products:
            copy_row(oms, 'products', p)
        for w in warehouses:
            copy_row(wms, 'warehouses', w)
        for n in range(1, ORDER_COUNT + 1):
            oid = BASE + n
            c = customers[(n - 1) % 5]
            status = 'partial_shipped' if n == 1 else 'pending' if n >= 19 else 'confirmed' if n >= 16 else 'shipped'
            order = dict(id=oid, order_no=f'UP-202611-{n:03d}', customer_id=c['id'], region_id=c['region_id'], office_id=c['office_id'], owner_employee_id=c['owner_employee_id'], channel_id=c['channel_id'], order_date=f'2026-11-{1 + (n-1)//2:02d}', due_date='2026-11-30', status=status, total_cents=0)
            items = []
            for j in range(2):
                p = products[(n + j - 1) % 6]
                price = p['list_price_cents']
                items.append(dict(id=BASE + n * 10 + j, order_id=oid, product_id=p['id'], quantity=2, unit_price_cents=price, unit_cost_cents=p['standard_cost_cents'], discount_cents=0, net_cents=2 * price))
            order['total_cents'] = sum(i['net_cents'] for i in items)
            insert(oms, 'sales_orders', order)
            insert(wms, 'order_references', dict(id=oid, order_no=order['order_no']))
            for item in items:
                insert(oms, 'sales_order_items', item)
                copy_row(wms, 'order_item_references', item)
            insert(oms, 'order_changes', dict(order_id=oid, change_seq=n))
            insert(oms, 'settlement_changes', dict(order_id=oid, change_seq=n))
            insert(wms, 'order_changes', dict(order_id=oid, change_seq=n))
            if n <= 15:
                warehouse_id = next(w['id'] for w in warehouses if w['office_id'] == c['office_id'])
                shipment = dict(id=oid, order_id=oid, warehouse_id=warehouse_id, shipped_date='2026-11-11', delivered_date='2026-11-12')
                insert(wms, 'shipments', shipment)
                for item in items:
                    insert(wms, 'shipment_items', dict(id=item['id'], shipment_id=oid, order_item_id=item['id'], quantity=1 if n == 1 else 2))
                amount = order['total_cents'] // 2 if n == 1 else order['total_cents']
                insert(oms, 'payments', dict(id=oid, customer_id=c['id'], region_id=c['region_id'], office_id=c['office_id'], owner_employee_id=c['owner_employee_id'], paid_date='2026-11-13', method='bank_transfer', amount_cents=amount))
                insert(oms, 'payment_allocations', dict(id=oid, payment_id=oid, order_id=oid, amount_cents=amount))
            if n == 2:
                insert(wms, 'sales_returns', dict(id=oid, order_id=oid, return_date='2026-11-14', reason='规格不合适', status='received'))
                insert(wms, 'sales_return_items', dict(id=oid, return_id=oid, order_item_id=items[0]['id'], quantity=1, amount_cents=items[0]['unit_price_cents']))
                insert(oms, 'refunds', dict(id=oid, return_id=oid, payment_id=oid, refund_date='2026-11-15', amount_cents=items[0]['unit_price_cents']))
        for n, (w, p) in enumerate(((w, p) for w in warehouses for p in products), 1):
            insert(wms, 'inventory_balances', dict(id=BASE+n, warehouse_id=w['id'], product_id=p['id'], on_hand=40+n, reserved=n % 4, snapshot_date='2026-11-15'))
            insert(wms, 'stock_changes', dict(stock_id=BASE+n, change_seq=n))
        insert(oms, 'demo_state', dict(id=1, phase=0))
        for db in [oms, wms]:
            for r in db.execute("SELECT name FROM sqlite_master WHERE type='table' AND name<>'schema_comments'").fetchall():
                insert(db, 'schema_comments', dict(table_name=r[0], column_name='', comment_zh='小规模关联抽取演示；金额为人民币分，change_seq 为最新变更序号'))
            if db.execute('PRAGMA foreign_key_check').fetchall():
                raise ValueError('Source foreign key check failed')
            db.commit()
        config = dict(version=1, database_id=database_id, connections=[
            dict(id='sales-transactions', name='销售交易演示来源', driver='sqlite', path=str(trading)),
            dict(id='warehouse-fulfillment', name='仓储履约演示来源', driver='sqlite', path=str(warehouse)),
        ], tasks=[])
        titles = {'orders': '关联演示：订单与明细', 'fulfillment': '关联演示：发货与退货', 'settlements': '关联演示：回款与退款', 'inventory': '关联演示：库存快照'}
        for kind in TABLES:
            db = oms if kind in ('orders', 'settlements') else wms
            config['tasks'].append(dict(id='supply-chain-'+kind, name=titles[kind], description='20 张关联订单的小规模演示。首次依次运行订单、履约、结算、库存；缺失依赖整次失败且不推进水位。', script=make_script(db, kind), sources=['sales-transactions' if db == oms else 'warehouse-fulfillment'], parameters=dict(batchSize=20, simulateFailure=False), schedule=dict(mode='interval', minutes=2 if kind == 'orders' else 3 if kind == 'fulfillment' else 5 if kind == 'settlements' else 10, timezone='Asia/Shanghai'), enabled=False, timeout=60))
        manifest = directory / 'sources.json'
        fd = os.open(manifest, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        made.append(manifest)
        with os.fdopen(fd, 'w') as f:
            json.dump(config, f, ensure_ascii=False, indent=2)
            f.write('\n')
        print(f'Created 2 sources: 5 customers, 6 products, 20 orders, 40 lines, 2 warehouses. Manifest: {manifest}')
    except Exception:
        for db in (oms, wms, source):
            if db is not None:
                db.close()
        for path in made:
            path.unlink(missing_ok=True)
        raise
    finally:
        for db in (oms, wms, source):
            if db is not None:
                db.close()


def advance(directory):
    # One SQLite transaction spanning the two local sources; no distributed server transaction.
    if not all((directory / name).is_file() for name in ('sales_transactions_demo.sqlite3', 'warehouse_fulfillment_demo.sqlite3')):
        raise ValueError('Create both sources before advancing')
    oms = connect(directory / 'sales_transactions_demo.sqlite3', 'rw')
    try:
        oms.execute('ATTACH DATABASE ? AS wms', (str((directory / 'warehouse_fulfillment_demo.sqlite3').resolve()),))
        oms.execute('BEGIN IMMEDIATE')
        phase = oms.execute('SELECT phase FROM demo_state WHERE id=1').fetchone()[0]
        if phase != 0:
            raise ValueError('Already advanced; no further events in this finite demo')
        oid = BASE + 1
        oms.execute("UPDATE sales_orders SET status='shipped' WHERE id=?", (oid,))
        oms.execute('UPDATE order_changes SET change_seq=21 WHERE order_id=?', (oid,))
        oms.execute("UPDATE sales_orders SET status='cancelled' WHERE id=?", (BASE+19,))
        oms.execute('UPDATE order_changes SET change_seq=22 WHERE order_id=?', (BASE+19,))
        oms.execute("INSERT INTO wms.shipments SELECT ?,order_id,warehouse_id,'2026-11-16','2026-11-16' FROM wms.shipments WHERE id=?", (BASE+101, oid))
        oms.execute('INSERT INTO wms.shipment_items SELECT id+1000,?,order_item_id,1 FROM wms.shipment_items WHERE shipment_id=?', (BASE+101, oid))
        oms.execute('UPDATE wms.order_changes SET change_seq=21 WHERE order_id=?', (oid,))
        oms.execute("INSERT INTO payments SELECT ?,customer_id,region_id,office_id,owner_employee_id,'2026-11-16',method,amount_cents FROM payments WHERE id=?", (BASE+101, oid))
        oms.execute('INSERT INTO payment_allocations SELECT ?,?,order_id,amount_cents FROM payment_allocations WHERE id=?', (BASE+101, BASE+101, oid))
        oms.execute('UPDATE settlement_changes SET change_seq=21 WHERE order_id=?', (oid,))
        oms.execute('UPDATE wms.inventory_balances SET on_hand=on_hand-2 WHERE id=?', (BASE+1,))
        oms.execute('UPDATE wms.stock_changes SET change_seq=13 WHERE stock_id=?', (BASE+1,))
        oms.execute('UPDATE demo_state SET phase=1 WHERE id=1')
        if oms.execute('PRAGMA foreign_key_check').fetchall() or oms.execute('PRAGMA wms.foreign_key_check').fetchall():
            raise ValueError('Source foreign key check failed')
        oms.commit()
        print('Advanced: order 001 fully shipped/paid, order 019 cancelled, one stock snapshot corrected. Run imports again.')
    finally:
        oms.close()


def backup_target(target, directory):
    directory.mkdir(parents=True, exist_ok=True)
    backup = directory / ('target-before-' + dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ') + '.sqlite3')
    fd = os.open(backup, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    os.close(fd)
    source = connect(target)
    try:
        with sqlite3.connect(backup) as out:
            source.backup(out)
    finally:
        source.close()
    print('Target backup:', backup)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--target', type=Path, default=ROOT/'data/regional_sales_demo.sqlite3')
    parser.add_argument('--directory', type=Path, default=ROOT/'data/supply-chain')
    parser.add_argument('--database-id', default='regional_sales_demo')
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument('--advance', action='store_true')
    mode.add_argument('--backup-target', action='store_true')
    args = parser.parse_args()
    try:
        if args.advance:
            advance(args.directory)
        elif args.backup_target:
            backup_target(args.target, args.directory)
        else:
            prepare(args.target, args.directory, args.database_id)
    except (ValueError, sqlite3.Error, OSError) as exc:
        parser.exit(1, str(exc) + '\n')
