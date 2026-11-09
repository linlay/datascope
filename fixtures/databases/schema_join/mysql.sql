-- Run as a fixture administrator. Create ds_join_reader separately with a
-- deployment-specific password; these schemas are dedicated acceptance data.
CREATE DATABASE ds_sales;
CREATE DATABASE ds_crm;
CREATE TABLE ds_sales.orders(order_id INTEGER PRIMARY KEY,customer_id INTEGER,owner_id INTEGER,secret VARCHAR(50));
CREATE TABLE ds_crm.customers(customer_id INTEGER PRIMARY KEY,customer_name VARCHAR(50),secret VARCHAR(50));
CREATE TABLE ds_sales.records(id INTEGER PRIMARY KEY,owner_label VARCHAR(20));
CREATE TABLE ds_crm.records(id INTEGER PRIMARY KEY,owner_label VARCHAR(20));
INSERT INTO ds_sales.orders VALUES(1,7,1,'sales secret');
INSERT INTO ds_crm.customers VALUES(7,'Alice','crm secret');
INSERT INTO ds_sales.records VALUES(1,'ds_sales');
INSERT INTO ds_crm.records VALUES(1,'ds_crm');
GRANT SELECT ON ds_sales.* TO 'ds_join_reader'@'%';
GRANT SELECT ON ds_crm.* TO 'ds_join_reader'@'%';
