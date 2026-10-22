PRAGMA foreign_keys = ON;
CREATE TABLE customers (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  email TEXT NOT NULL UNIQUE,
  owner_employee_id INTEGER NOT NULL
);
CREATE TABLE products (
  id INTEGER PRIMARY KEY,
  sku TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  price_cents INTEGER NOT NULL CHECK(price_cents >= 0),
  cost_cents INTEGER NOT NULL CHECK(cost_cents >= 0),
  stock_quantity INTEGER NOT NULL CHECK(stock_quantity >= 0)
);
CREATE TABLE orders (
  id INTEGER PRIMARY KEY,
  order_no TEXT NOT NULL UNIQUE,
  customer_id INTEGER NOT NULL REFERENCES customers(id),
  product_id INTEGER NOT NULL REFERENCES products(id),
  quantity INTEGER NOT NULL CHECK(quantity > 0),
  unit_price_cents INTEGER NOT NULL CHECK(unit_price_cents >= 0),
  status TEXT NOT NULL CHECK(status IN ('pending', 'paid', 'shipped', 'cancelled')),
  created_at TEXT NOT NULL
);
-- All records are fictional. Amounts are integer cents.
INSERT INTO customers VALUES
  (1, '示例客户甲', 'customer1@example.invalid', 1),
  (2, '示例客户乙', 'customer2@example.invalid', 1),
  (3, '示例客户丙', 'customer3@example.invalid', 2),
  (4, '示例客户丁', 'customer4@example.invalid', 2);
INSERT INTO products VALUES
  (1, 'DEMO-001', '桌面笔记本', 1800, 700, 120),
  (2, 'DEMO-002', '中性笔套装', 2500, 1000, 80),
  (3, 'DEMO-003', '便携水杯', 5900, 2800, 45),
  (4, 'DEMO-004', '帆布收纳袋', 3900, 1600, 60),
  (5, 'DEMO-005', '桌面支架', 8900, 4200, 30),
  (6, 'DEMO-006', '阅读书签', 1200, 300, 200);
INSERT INTO orders VALUES
  (1, 'SHOP-20261022-001', 1, 1, 2, 1800, 'paid', '2026-10-22T09:00:00+08:00'),
  (2, 'SHOP-20261022-002', 1, 3, 1, 5900, 'shipped', '2026-10-22T09:15:00+08:00'),
  (3, 'SHOP-20261022-003', 2, 2, 3, 2500, 'paid', '2026-10-22T10:00:00+08:00'),
  (4, 'SHOP-20261022-004', 2, 6, 2, 1200, 'pending', '2026-10-22T10:30:00+08:00'),
  (5, 'SHOP-20261022-005', 3, 5, 1, 8900, 'paid', '2026-10-22T11:00:00+08:00'),
  (6, 'SHOP-20261022-006', 3, 4, 2, 3900, 'shipped', '2026-10-22T11:30:00+08:00'),
  (7, 'SHOP-20261022-007', 4, 3, 1, 5900, 'cancelled', '2026-10-22T12:00:00+08:00'),
  (8, 'SHOP-20261022-008', 4, 1, 4, 1800, 'pending', '2026-10-22T12:30:00+08:00');
CREATE INDEX orders_customer_idx ON orders(customer_id);
CREATE INDEX orders_product_idx ON orders(product_id);
