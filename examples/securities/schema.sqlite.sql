PRAGMA foreign_keys=ON;

CREATE TABLE roles(
  id INTEGER PRIMARY KEY,
  code TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  description TEXT NOT NULL
);

CREATE TABLE branches(
  code TEXT NOT NULL PRIMARY KEY,
  name TEXT NOT NULL,
  city TEXT NOT NULL,
  address TEXT NOT NULL
);

CREATE TABLE departments(
  code TEXT NOT NULL PRIMARY KEY,
  branch_code TEXT NOT NULL REFERENCES branches(code),
  name TEXT NOT NULL,
  address TEXT NOT NULL
);

CREATE TABLE employees(
  employee_id INTEGER PRIMARY KEY,
  emp_no TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  role_id INTEGER NOT NULL REFERENCES roles(id),
  branch_code TEXT REFERENCES branches(code),
  department_code TEXT REFERENCES departments(code),
  title TEXT NOT NULL,
  mobile TEXT NOT NULL,
  hire_date TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('active','resigned'))
);

CREATE TABLE employee_scopes(
  id INTEGER PRIMARY KEY,
  employee_id INTEGER NOT NULL REFERENCES employees(employee_id),
  branch_code TEXT REFERENCES branches(code),
  department_code TEXT REFERENCES departments(code),
  effective_date TEXT NOT NULL,
  expiry_date TEXT,
  CHECK((branch_code IS NULL) != (department_code IS NULL)),
  CHECK(expiry_date IS NULL OR expiry_date>effective_date)
);

CREATE TABLE customers(
  customer_no TEXT NOT NULL PRIMARY KEY,
  customer_type TEXT NOT NULL CHECK(customer_type IN ('INDIVIDUAL','CORPORATE')),
  name TEXT NOT NULL,
  gender TEXT NOT NULL,
  birth_date TEXT NOT NULL,
  id_type TEXT NOT NULL,
  id_no TEXT NOT NULL,
  mobile TEXT NOT NULL,
  risk_level TEXT NOT NULL CHECK(risk_level IN ('C1','C2','C3','C4','C5')),
  investor_type TEXT NOT NULL,
  open_date TEXT NOT NULL,
  status TEXT NOT NULL
);

CREATE TABLE fund_accounts(
  fund_account_no TEXT NOT NULL PRIMARY KEY,
  customer_no TEXT NOT NULL REFERENCES customers(customer_no),
  department_code TEXT NOT NULL REFERENCES departments(code),
  manager_id INTEGER NOT NULL REFERENCES employees(employee_id),
  currency TEXT NOT NULL,
  status TEXT NOT NULL
);

CREATE TABLE stock_accounts(
  id INTEGER PRIMARY KEY,
  fund_account_no TEXT NOT NULL REFERENCES fund_accounts(fund_account_no),
  exchange TEXT NOT NULL CHECK(exchange IN ('SH','SZ')),
  stock_account_no TEXT NOT NULL UNIQUE,
  UNIQUE(fund_account_no,exchange)
);

CREATE TABLE securities(
  code TEXT NOT NULL PRIMARY KEY,
  name TEXT NOT NULL,
  exchange TEXT NOT NULL CHECK(exchange IN ('SH','SZ')),
  board TEXT NOT NULL,
  security_type TEXT NOT NULL,
  lot_size INTEGER NOT NULL,
  min_buy_quantity INTEGER NOT NULL
);

CREATE TABLE trading_calendar(
  trade_date TEXT PRIMARY KEY,
  is_open INTEGER NOT NULL CHECK(is_open IN(0,1))
);

CREATE TABLE daily_quotes(
  id INTEGER PRIMARY KEY,
  security_code TEXT NOT NULL REFERENCES securities(code),
  trade_date TEXT NOT NULL REFERENCES trading_calendar(trade_date),
  prev_close INTEGER NOT NULL,
  open INTEGER NOT NULL,
  high INTEGER NOT NULL,
  low INTEGER NOT NULL,
  close INTEGER NOT NULL,
  volume INTEGER NOT NULL,
  amount INTEGER NOT NULL,
  UNIQUE(security_code,trade_date),
  CHECK(low<=open AND low<=close AND high>=open AND high>=close)
);

CREATE TABLE orders(
  id INTEGER PRIMARY KEY,
  order_no TEXT NOT NULL UNIQUE,
  trade_date TEXT NOT NULL REFERENCES trading_calendar(trade_date),
  order_time TEXT NOT NULL,
  customer_no TEXT NOT NULL REFERENCES customers(customer_no),
  fund_account_no TEXT NOT NULL REFERENCES fund_accounts(fund_account_no),
  stock_account_id INTEGER NOT NULL REFERENCES stock_accounts(id),
  security_code TEXT NOT NULL REFERENCES securities(code),
  side TEXT NOT NULL CHECK(side IN('BUY','SELL')),
  price INTEGER NOT NULL,
  quantity INTEGER NOT NULL CHECK(quantity>0),
  filled_quantity INTEGER NOT NULL CHECK(filled_quantity>=0 AND filled_quantity<=quantity),
  status TEXT NOT NULL CHECK(status IN('FILLED','PARTIAL','CANCELLED','PART_CANCELLED','REJECTED')),
  channel TEXT NOT NULL
);

CREATE TABLE trades(
  id INTEGER PRIMARY KEY,
  trade_no TEXT NOT NULL UNIQUE,
  order_id INTEGER NOT NULL REFERENCES orders(id),
  customer_no TEXT NOT NULL REFERENCES customers(customer_no),
  fund_account_no TEXT NOT NULL REFERENCES fund_accounts(fund_account_no),
  security_code TEXT NOT NULL REFERENCES securities(code),
  trade_date TEXT NOT NULL,
  trade_time TEXT NOT NULL,
  side TEXT NOT NULL,
  price INTEGER NOT NULL,
  quantity INTEGER NOT NULL CHECK(quantity>0),
  amount_cents INTEGER NOT NULL,
  commission_cents INTEGER NOT NULL,
  stamp_tax_cents INTEGER NOT NULL,
  transfer_fee_cents INTEGER NOT NULL,
  fee_cents INTEGER NOT NULL,
  CHECK(amount_cents=price*quantity),
  CHECK(fee_cents=commission_cents+stamp_tax_cents+transfer_fee_cents)
);

CREATE TABLE cash_flows(
  id INTEGER PRIMARY KEY,
  customer_no TEXT NOT NULL REFERENCES customers(customer_no),
  fund_account_no TEXT NOT NULL REFERENCES fund_accounts(fund_account_no),
  trade_date TEXT NOT NULL,
  flow_type TEXT NOT NULL CHECK(flow_type IN('DEPOSIT','WITHDRAWAL','BUY','SELL')),
  trade_id INTEGER REFERENCES trades(id),
  amount_cents INTEGER NOT NULL,
  fee_cents INTEGER NOT NULL,
  balance_after INTEGER NOT NULL CHECK(balance_after>=0)
);

CREATE TABLE positions(
  id INTEGER PRIMARY KEY,
  customer_no TEXT NOT NULL REFERENCES customers(customer_no),
  fund_account_no TEXT NOT NULL REFERENCES fund_accounts(fund_account_no),
  security_code TEXT NOT NULL REFERENCES securities(code),
  trade_date TEXT NOT NULL,
  opening_quantity INTEGER NOT NULL,
  buy_quantity INTEGER NOT NULL,
  sell_quantity INTEGER NOT NULL,
  quantity INTEGER NOT NULL CHECK(quantity>=0),
  available_quantity INTEGER NOT NULL CHECK(available_quantity>=0 AND available_quantity<=quantity),
  cost_cents INTEGER NOT NULL,
  close_price INTEGER NOT NULL,
  market_value_cents INTEGER NOT NULL,
  UNIQUE(fund_account_no,security_code,trade_date),
  CHECK(quantity=opening_quantity+buy_quantity-sell_quantity),
  CHECK(market_value_cents=quantity*close_price)
);

CREATE TABLE daily_balances(
  id INTEGER PRIMARY KEY,
  customer_no TEXT NOT NULL REFERENCES customers(customer_no),
  fund_account_no TEXT NOT NULL REFERENCES fund_accounts(fund_account_no),
  trade_date TEXT NOT NULL,
  opening_balance INTEGER NOT NULL,
  transfer_in INTEGER NOT NULL,
  transfer_out INTEGER NOT NULL,
  buy_amount INTEGER NOT NULL,
  sell_amount INTEGER NOT NULL,
  fees INTEGER NOT NULL,
  closing_balance INTEGER NOT NULL CHECK(closing_balance>=0),
  available_balance INTEGER NOT NULL,
  withdrawable_balance INTEGER NOT NULL,
  UNIQUE(fund_account_no,trade_date),
  CHECK(closing_balance=opening_balance+transfer_in-transfer_out-buy_amount+sell_amount-fees)
);

CREATE TABLE schema_comments(
  table_name TEXT NOT NULL,
  column_name TEXT NOT NULL,
  comment_zh TEXT NOT NULL,
  PRIMARY KEY(table_name,column_name)
);

CREATE INDEX idx_scopes_employee ON employee_scopes(employee_id,effective_date,expiry_date);

CREATE INDEX idx_fund_accounts_manager ON fund_accounts(manager_id);

CREATE INDEX idx_fund_accounts_department ON fund_accounts(department_code);

CREATE INDEX idx_employees_department ON employees(department_code);

CREATE INDEX idx_orders_customer ON orders(customer_no,trade_date);

CREATE INDEX idx_trades_customer ON trades(customer_no,trade_date);

CREATE INDEX idx_flows_customer ON cash_flows(customer_no,trade_date);

CREATE INDEX idx_positions_customer ON positions(customer_no,trade_date);

CREATE INDEX idx_balances_customer ON daily_balances(customer_no,trade_date);

CREATE INDEX idx_trades_order ON trades(order_id);

CREATE INDEX idx_fund_accounts_customer ON fund_accounts(customer_no);
CREATE INDEX idx_trades_account ON trades(fund_account_no,trade_date);
CREATE INDEX idx_flows_account ON cash_flows(fund_account_no,id);
