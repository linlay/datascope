-- Small fictional WMS. Order references come from sales-transactions.
PRAGMA foreign_keys=ON;
CREATE TABLE order_references(id INTEGER PRIMARY KEY,order_no TEXT NOT NULL UNIQUE);
CREATE TABLE order_item_references(id INTEGER PRIMARY KEY,order_id INTEGER NOT NULL REFERENCES order_references(id),product_id INTEGER NOT NULL,quantity INTEGER NOT NULL CHECK(quantity>0));
CREATE TABLE warehouses(id INTEGER PRIMARY KEY,name TEXT NOT NULL,region_id INTEGER NOT NULL,office_id INTEGER NOT NULL);
CREATE TABLE shipments(id INTEGER PRIMARY KEY,order_id INTEGER NOT NULL REFERENCES order_references(id),warehouse_id INTEGER NOT NULL REFERENCES warehouses(id),shipped_date TEXT NOT NULL,delivered_date TEXT NOT NULL,CHECK(delivered_date>=shipped_date));
CREATE TABLE shipment_items(id INTEGER PRIMARY KEY,shipment_id INTEGER NOT NULL REFERENCES shipments(id),order_item_id INTEGER NOT NULL REFERENCES order_item_references(id),quantity INTEGER NOT NULL CHECK(quantity>0));
CREATE TABLE sales_returns(id INTEGER PRIMARY KEY,order_id INTEGER NOT NULL REFERENCES order_references(id),return_date TEXT NOT NULL,reason TEXT NOT NULL,status TEXT NOT NULL CHECK(status IN ('requested','approved','received','rejected')));
CREATE TABLE sales_return_items(id INTEGER PRIMARY KEY,return_id INTEGER NOT NULL REFERENCES sales_returns(id),order_item_id INTEGER NOT NULL REFERENCES order_item_references(id),quantity INTEGER NOT NULL CHECK(quantity>0),amount_cents INTEGER NOT NULL CHECK(amount_cents>0));
CREATE TABLE inventory_balances(id INTEGER PRIMARY KEY,warehouse_id INTEGER NOT NULL REFERENCES warehouses(id),product_id INTEGER NOT NULL,on_hand INTEGER NOT NULL CHECK(on_hand>=0),reserved INTEGER NOT NULL CHECK(reserved BETWEEN 0 AND on_hand),snapshot_date TEXT NOT NULL,UNIQUE(warehouse_id,product_id,snapshot_date));
CREATE TABLE order_changes(order_id INTEGER PRIMARY KEY REFERENCES order_references(id),change_seq INTEGER NOT NULL UNIQUE);
CREATE TABLE stock_changes(stock_id INTEGER PRIMARY KEY REFERENCES inventory_balances(id),change_seq INTEGER NOT NULL UNIQUE);
CREATE TABLE schema_comments(table_name TEXT NOT NULL,column_name TEXT NOT NULL,comment_zh TEXT NOT NULL,PRIMARY KEY(table_name,column_name));
