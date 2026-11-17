-- Small fictional OMS. Reference IDs map to the selected regional sales database.
PRAGMA foreign_keys=ON;
CREATE TABLE customers(id INTEGER PRIMARY KEY,customer_no TEXT NOT NULL UNIQUE,name TEXT NOT NULL,region_id INTEGER NOT NULL,office_id INTEGER NOT NULL,owner_employee_id INTEGER NOT NULL,channel_id INTEGER NOT NULL);
CREATE TABLE products(id INTEGER PRIMARY KEY,sku TEXT NOT NULL UNIQUE,name TEXT NOT NULL,list_price_cents INTEGER NOT NULL,standard_cost_cents INTEGER NOT NULL);
CREATE TABLE sales_orders(id INTEGER PRIMARY KEY,order_no TEXT NOT NULL UNIQUE,customer_id INTEGER NOT NULL REFERENCES customers(id),region_id INTEGER NOT NULL,office_id INTEGER NOT NULL,owner_employee_id INTEGER NOT NULL,channel_id INTEGER NOT NULL,order_date TEXT NOT NULL,due_date TEXT NOT NULL,status TEXT NOT NULL CHECK(status IN ('pending','confirmed','partial_shipped','shipped','cancelled')),total_cents INTEGER NOT NULL CHECK(total_cents>=0));
CREATE TABLE sales_order_items(id INTEGER PRIMARY KEY,order_id INTEGER NOT NULL REFERENCES sales_orders(id),product_id INTEGER NOT NULL REFERENCES products(id),quantity INTEGER NOT NULL CHECK(quantity>0),unit_price_cents INTEGER NOT NULL,unit_cost_cents INTEGER NOT NULL,discount_cents INTEGER NOT NULL,net_cents INTEGER NOT NULL CHECK(net_cents=quantity*unit_price_cents-discount_cents));
CREATE TABLE payments(id INTEGER PRIMARY KEY,customer_id INTEGER NOT NULL REFERENCES customers(id),region_id INTEGER NOT NULL,office_id INTEGER NOT NULL,owner_employee_id INTEGER NOT NULL,paid_date TEXT NOT NULL,method TEXT NOT NULL,amount_cents INTEGER NOT NULL CHECK(amount_cents>0));
CREATE TABLE payment_allocations(id INTEGER PRIMARY KEY,payment_id INTEGER NOT NULL REFERENCES payments(id),order_id INTEGER NOT NULL REFERENCES sales_orders(id),amount_cents INTEGER NOT NULL CHECK(amount_cents>0),UNIQUE(payment_id,order_id));
-- return_id refers to WMS sales_returns; there is no cross-file foreign key.
CREATE TABLE refunds(id INTEGER PRIMARY KEY,return_id INTEGER NOT NULL,payment_id INTEGER NOT NULL REFERENCES payments(id),refund_date TEXT NOT NULL,amount_cents INTEGER NOT NULL CHECK(amount_cents>0));
-- Latest version per order, not a complete event history.
CREATE TABLE order_changes(order_id INTEGER PRIMARY KEY REFERENCES sales_orders(id),change_seq INTEGER NOT NULL UNIQUE);
CREATE TABLE settlement_changes(order_id INTEGER PRIMARY KEY REFERENCES sales_orders(id),change_seq INTEGER NOT NULL UNIQUE);
CREATE TABLE demo_state(id INTEGER PRIMARY KEY CHECK(id=1),phase INTEGER NOT NULL);
CREATE TABLE schema_comments(table_name TEXT NOT NULL,column_name TEXT NOT NULL,comment_zh TEXT NOT NULL,PRIMARY KEY(table_name,column_name));
