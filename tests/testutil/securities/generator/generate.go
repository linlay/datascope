// Package gendata builds deterministic, synthetic securities brokerage data.
package gendata

import (
	"database/sql"
	"fmt"
	"math"
	"math/rand"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Options struct {
	Path, Schema string
	Customers    int
	Seed         int64
}

// Generate refuses to overwrite any existing database. A failed generation removes its partial file.
func Generate(o Options) (err error) {
	if o.Customers <= 0 {
		o.Customers = 1500
	}
	if o.Seed == 0 {
		o.Seed = 20261021
	}
	if err = os.MkdirAll(filepath.Dir(o.Path), 0755); err != nil {
		return
	}
	f, e := os.OpenFile(o.Path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	f.Close()
	defer func() {
		if err != nil {
			os.Remove(o.Path)
		}
	}()
	db, e := sql.Open("sqlite", o.Path)
	if e != nil {
		return e
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ddl, e := os.ReadFile(o.Schema)
	if e != nil {
		return e
	}
	if _, err = db.Exec(string(ddl)); err != nil {
		return
	}
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	inserts := map[string]*sql.Stmt{}
	put := func(table string, args ...any) {
		if err != nil {
			return
		}
		s := inserts[table]
		if s == nil {
			q := "INSERT INTO " + table + " VALUES (" + strings.TrimSuffix(strings.Repeat("?,", len(args)), ",") + ")"
			s, err = tx.Prepare(q)
			inserts[table] = s
		}
		if err == nil {
			_, err = s.Exec(args...)
		}
	}
	defer func() {
		for _, s := range inserts {
			if s != nil {
				s.Close()
			}
		}
	}()
	orgCode := func(prefix string, id any) any {
		if id == nil {
			return nil
		}
		return fmt.Sprintf("%s%03d", prefix, id)
	}
	roles := []string{"客户经理", "营业部主管", "分公司主管", "总部分管人员", "系统管理员"}
	codes := []string{"MANAGER", "DEPARTMENT_HEAD", "BRANCH_HEAD", "HQ_SUPERVISOR", "ADMIN"}
	for i, n := range roles {
		put("roles", i+1, codes[i], n, n+"（合成演示）")
	}
	cities := []string{"上海", "杭州", "南京", "苏州", "北京", "天津", "广州", "深圳", "成都", "武汉"}
	for b := 1; b <= 10; b++ {
		put("branches", fmt.Sprintf("B%03d", b), "星澜证券"+cities[b-1]+"分公司", cities[b-1], cities[b-1]+"市人民路100号（虚构机构）")
	}
	for d := 1; d <= 30; d++ {
		b := (d-1)/3 + 1
		put("departments", fmt.Sprintf("D%03d", d), orgCode("B", b), cities[b-1]+fmt.Sprintf("第%d营业部", (d-1)%3+1), cities[b-1]+"市中山路88号（演示）")
	}
	surnames := []string{"陈", "王", "李", "张", "刘", "赵", "周", "徐", "孙", "林"}
	given := []string{"晓明", "雨桐", "子涵", "晨曦", "宇航", "佳宁", "思远", "若溪", "嘉诚", "文静", "浩然", "欣怡", "梓轩", "诗涵", "明哲"}
	name := func(i int) string { return surnames[(i/15)%10] + given[i%15] }
	employeeIDs := []int{}
	for i := 1; i <= 50; i++ {
		employeeIDs = append(employeeIDs, i)
	}
	for i := 121; i <= 166; i++ {
		employeeIDs = append(employeeIDs, i)
	}
	for i := 175; i <= 178; i++ {
		employeeIDs = append(employeeIDs, i)
	}
	for _, i := range employeeIDs {
		role := 1
		var b, d any
		title := roles[0]
		status := "active"
		if i <= 50 {
			d = (i-1)%30 + 1
			b = ((i-1)%30)/3 + 1
		} else if i <= 150 {
			role = 2
			d = i - 120
			b = (i-121)/3 + 1
		} else if i <= 160 {
			role = 3
			b = i - 150
		} else if i <= 166 {
			role = 4
		} else if i == 175 {
			role = 5
		} else {
			status = "resigned"
			d = i - 175
			b = 1
		}
		title = roles[role-1]
		put("employees", i, fmt.Sprintf("E%04d", i), name(i), role, orgCode("B", b), orgCode("D", d), title, fmt.Sprintf("138****%04d", i), "2020-03-16", status)
	}
	scope := 0
	addScope := func(e int, b, d any, start string, end any) {
		scope++
		put("employee_scopes", scope, e, orgCode("B", b), orgCode("D", d), start, end)
	}
	for i := 121; i <= 150; i++ {
		addScope(i, nil, i-120, "2020-01-01", nil)
	}
	for i := 151; i <= 160; i++ {
		addScope(i, i-150, nil, "2020-01-01", nil)
	}
	for i := 161; i <= 166; i++ {
		if i%2 == 1 {
			for j := 0; j < 3; j++ {
				addScope(i, (i-161+j)%10+1, nil, "2020-01-01", nil)
			}
		} else {
			for j := 0; j < 5; j++ {
				addScope(i, nil, (i-162+j*3)%30+1, "2020-01-01", nil)
			}
		}
	}
	addScope(121, 10, nil, "2020-01-01", "2026-10-01")
	addScope(121, 9, nil, "2026-11-01", nil)
	r := rand.New(rand.NewSource(o.Seed))
	type holding struct{ qty, cost int64 }
	type account struct {
		customerNo, fundAccountNo string
		stockAccountID            int
		cash                      int64
		holdings                  map[int]*holding
		activity                  float64
	}
	cs := []account{}
	const ns = 60
	prices := make([]int64, ns)
	securityCodes := make([]string, ns)
	prefixes := []string{"华辰", "远航", "中瑞", "海川", "景和", "东岳", "嘉信", "新源", "恒泰", "盛达"}
	industries := []string{"科技", "实业", "发展", "银行", "证券", "期货", "保险", "医药", "能源", "制造"}
	for s := 0; s < ns; s++ {
		ex, board, code := "SH", "MAIN", fmt.Sprintf("600%03d", s+1)
		minq := 100
		if s >= 30 {
			ex = "SZ"
			code = fmt.Sprintf("000%03d", s-29)
		}
		if s < 10 {
			board = "STAR"
			code = fmt.Sprintf("688%03d", s+1)
			minq = 200
		}
		prices[s] = int64(800 + r.Intn(12000))
		securityCodes[s] = code
		securityName := prefixes[s%10] + industries[(s/10+s%10)%10]
		if board == "STAR" {
			securityName = prefixes[s%10] + []string{"科技", "电子", "新材", "软件", "半导体", "智能", "生物", "光电", "精密", "芯能"}[s]
		}
		put("securities", code, securityName, ex, board, "A_SHARE", 100, minq)
	}
	for i := 0; i < o.Customers; i++ {
		id := i + 1
		birth := fmt.Sprintf("%d-%02d-%02d", 1960+i%44, 1+i%12, 1+i%28)
		customerNo := fmt.Sprintf("C%08d", id)
		customerType, customerName, gender, idType := "INDIVIDUAL", name(i), []string{"男", "女"}[i%2], "居民身份证"
		idNo := fmt.Sprintf("310115********%03dX", i%1000)
		accountCount := 1
		if i%20 == 0 {
			customerType, gender, birth, idType = "CORPORATE", "不适用", "", "统一社会信用代码"
			customerName = cities[(i/20)%10] + prefixes[(i/200)%10] + industries[(i/20)%10] + "有限公司"
			idNo = fmt.Sprintf("91310000******%04d", id)
			accountCount = 4 + (i/20)%7
		} else if i%13 == 0 {
			accountCount = 2 + (i/13)%2
		}
		put("customers", customerNo, customerType, customerName, gender, birth, idType, idNo, fmt.Sprintf("139****%04d", id), fmt.Sprintf("C%d", 1+i%5), []string{"普通", "普通", "普通", "专业"}[i%4], "2025-01-06", "active")
		for a := 0; a < accountCount; a++ {
			fundAccountNo := fmt.Sprintf("8800%08d", len(cs)+1)
			// A customer's accounts may belong to different departments and managers.
			d := (i+a*3)%30 + 1
			m := d
			if d <= 20 {
				m += 30 * ((i/30 + a) % 2)
			}
			if (i+a)%97 == 0 {
				m = (m+11)%50 + 1
			}
			put("fund_accounts", fundAccountNo, customerNo, orgCode("D", d), m, "CNY", "active")
			accountID := len(cs) + 1
			put("stock_accounts", accountID*2-1, fundAccountNo, "SH", fmt.Sprintf("A%09d", accountID))
			put("stock_accounts", accountID*2, fundAccountNo, "SZ", fmt.Sprintf("%010d", accountID))
			cash := int64(math.Min(500000000, math.Max(5000000, math.Exp(r.NormFloat64()*1.1+math.Log(35000000)))))
			c := account{customerNo: customerNo, fundAccountNo: fundAccountNo, stockAccountID: accountID*2 - 1, cash: cash, holdings: map[int]*holding{}, activity: []float64{.9, .55, .12}[(i+a)%3]}
			for j := 0; j < 2+(i+a)%2; j++ {
				s := ((i+a)*7 + j*13) % ns
				q := int64(200 + r.Intn(5)*100)
				c.holdings[s] = &holding{q, q * prices[s]}
			}
			cs = append(cs, c)
		}
	}

	orderID, tradeID, flowID, posID, balID, quoteID := 0, 0, 0, 0, 0, 0
	start := time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		date := day.Format("2006-01-02")
		open := day.Weekday() != time.Saturday && day.Weekday() != time.Sunday && date != "2026-06-19" && date != "2026-09-25" && !(date >= "2026-10-01" && date <= "2026-10-07")
		isopen := 0
		if open {
			isopen = 1
		}
		put("trading_calendar", date, isopen)
		if !open {
			continue
		}
		lows, highs := make([]int64, ns), make([]int64, ns)
		for s := 0; s < ns; s++ {
			prev := prices[s]
			cl := max(int64(100), prev+int64(r.Intn(401)-200)*prev/10000)
			op := max(int64(100), prev+int64(r.Intn(201)-100)*prev/10000)
			lo := min(op, cl) * 98 / 100
			hi := max(op, cl) * 102 / 100
			vol := int64(100000 + r.Intn(9000000))
			quoteID++
			put("daily_quotes", quoteID, securityCodes[s], date, prev, op, hi, lo, cl, vol, vol*cl)
			prices[s] = cl
			lows[s] = lo
			highs[s] = hi
		}
		for i := range cs {
			c := &cs[i]
			opening := c.cash
			var buy, sell, fees, tin, tout int64
			openingQty := map[int]int64{}
			buys, sells := map[int]int64{}, map[int]int64{}
			// Iterate security ids in order: map iteration must not affect seeded results.
			for s := 0; s < ns; s++ {
				if h := c.holdings[s]; h != nil {
					openingQty[s] = h.qty
				}
			}
			if r.Intn(500) == 0 {
				tin = 1000000
				c.cash += tin
				flowID++
				put("cash_flows", flowID, c.customerNo, c.fundAccountNo, date, "DEPOSIT", nil, tin, 0, c.cash)
			}
			if r.Intn(1000) == 0 && c.cash > 2000000 {
				tout = 500000
				c.cash -= tout
				flowID++
				put("cash_flows", flowID, c.customerNo, c.fundAccountNo, date, "WITHDRAWAL", nil, -tout, 0, c.cash)
			}
			attempts := 1
			if i%3 == 0 {
				attempts = 2
			}
			for a := 0; a < attempts; a++ {
				if r.Float64() > c.activity {
					continue
				}
				s := (i*7 + r.Intn(3)*13) % ns
				side := "BUY"
				if r.Intn(2) == 0 {
					side = "SELL"
				}
				q := int64(200 + r.Intn(5)*100)
				price := lows[s] + r.Int63n(highs[s]-lows[s]+1)
				filled := q
				status := "FILLED"
				roll := r.Intn(100)
				if roll < 8 {
					filled = 0
					status = "CANCELLED"
				} else if roll < 12 {
					filled = (q / 200) * 100
					status = "PART_CANCELLED"
				} else if roll < 15 {
					filled = (q / 200) * 100
					status = "PARTIAL"
				}
				if side == "SELL" && openingQty[s]-sells[s] < q || side == "BUY" && c.cash < price*q+max(int64(500), price*q*25/100000)+price*q/100000+5 {
					filled = 0
					status = "REJECTED"
				}
				orderID++
				sa := c.stockAccountID
				if s >= 30 {
					sa++
				}
				tm := fmt.Sprintf("%sT%02d:%02d:%02d+08:00", date, 9+(a+1)*2, 30+r.Intn(25), r.Intn(60))
				put("orders", orderID, fmt.Sprintf("O%010d", orderID), date, tm, c.customerNo, c.fundAccountNo, sa, securityCodes[s], side, price, q, filled, status, []string{"APP", "PC", "COUNTER"}[r.Intn(3)])
				if filled == 0 {
					continue
				}
				parts := []int64{filled}
				if filled >= 400 && r.Intn(4) == 0 {
					parts = []int64{100, filled - 100}
				}
				commission := max(int64(500), (price*filled*25+50000)/100000)
				for part, n := range parts {
					amount := price * n
					com := commission * n / filled
					if part == len(parts)-1 {
						com = commission
					}
					commission -= com
					stamp := int64(0)
					if side == "SELL" {
						stamp = (amount*5 + 5000) / 10000
					}
					transfer := (amount + 50000) / 100000
					fee := com + stamp + transfer
					tradeID++
					put("trades", tradeID, fmt.Sprintf("T%010d", tradeID), orderID, c.customerNo, c.fundAccountNo, securityCodes[s], date, tm, side, price, n, amount, com, stamp, transfer, fee)
					h := c.holdings[s]
					if h == nil {
						h = &holding{}
						c.holdings[s] = h
					}
					delta := -amount - fee
					if side == "BUY" {
						buy += amount
						buys[s] += n
						h.qty += n
						h.cost += amount + fee
					} else {
						sell += amount
						sells[s] += n
						if h.qty > 0 {
							h.cost -= h.cost * n / h.qty
						}
						h.qty -= n
						delta = amount - fee
					}
					c.cash += delta
					fees += fee
					flowID++
					put("cash_flows", flowID, c.customerNo, c.fundAccountNo, date, side, tradeID, delta, fee, c.cash)
				}
			}
			for s := 0; s < ns; s++ {
				h := c.holdings[s]
				if h == nil || h.qty == 0 && openingQty[s] == 0 && buys[s] == 0 {
					continue
				}
				posID++
				put("positions", posID, c.customerNo, c.fundAccountNo, securityCodes[s], date, openingQty[s], buys[s], sells[s], h.qty, h.qty-buys[s], h.cost, prices[s], h.qty*prices[s])
			}
			balID++
			put("daily_balances", balID, c.customerNo, c.fundAccountNo, date, opening, tin, tout, buy, sell, fees, c.cash, c.cash, max(int64(0), c.cash-sell))
		}
	}
	if err != nil {
		return
	}
	if err = comments(tx); err != nil {
		return
	}
	if err = validate(tx); err != nil {
		return
	}
	return tx.Commit()
}

func validate(tx *sql.Tx) error {
	checks := []string{
		`SELECT count(*) FROM pragma_foreign_key_check`,
		`SELECT count(*) FROM cash_flows f JOIN fund_accounts a ON a.fund_account_no=f.fund_account_no LEFT JOIN trades t ON t.id=f.trade_id WHERE a.customer_no!=f.customer_no OR (f.trade_id IS NOT NULL AND (t.customer_no!=f.customer_no OR t.fund_account_no!=f.fund_account_no OR t.trade_date!=f.trade_date))`,
		`SELECT count(*) FROM positions p JOIN fund_accounts a ON a.fund_account_no=p.fund_account_no WHERE a.customer_no!=p.customer_no`,
		`SELECT count(*) FROM daily_balances b JOIN fund_accounts a ON a.fund_account_no=b.fund_account_no WHERE a.customer_no!=b.customer_no`,
		`SELECT count(*) FROM (SELECT fund_account_no,trade_date,amount_cents,balance_after,LAG(balance_after) OVER(PARTITION BY fund_account_no ORDER BY id) AS prev FROM cash_flows) f JOIN daily_balances b ON b.fund_account_no=f.fund_account_no AND b.trade_date=f.trade_date WHERE f.balance_after!=COALESCE(f.prev,b.opening_balance)+f.amount_cents`,
		`SELECT count(*) FROM positions p WHERE p.buy_quantity!=COALESCE((SELECT sum(t.quantity) FROM trades t WHERE t.fund_account_no=p.fund_account_no AND t.security_code=p.security_code AND t.trade_date=p.trade_date AND t.side='BUY'),0) OR p.sell_quantity!=COALESCE((SELECT sum(t.quantity) FROM trades t WHERE t.fund_account_no=p.fund_account_no AND t.security_code=p.security_code AND t.trade_date=p.trade_date AND t.side='SELL'),0)`,
		`SELECT count(*) FROM orders o JOIN fund_accounts f ON f.fund_account_no=o.fund_account_no JOIN stock_accounts a ON a.id=o.stock_account_id JOIN securities s ON s.code=o.security_code WHERE f.customer_no!=o.customer_no OR a.fund_account_no!=o.fund_account_no OR a.exchange!=s.exchange OR (s.board='STAR' AND o.side='BUY' AND o.quantity<200)`,
		`SELECT count(*) FROM orders o WHERE filled_quantity>0 AND (SELECT sum(commission_cents) FROM trades t WHERE t.order_id=o.id)!=max(500,(o.price*o.filled_quantity*25+50000)/100000)`,
		`SELECT count(*) FROM trades t JOIN orders o ON o.id=t.order_id WHERE t.customer_no!=o.customer_no OR t.fund_account_no!=o.fund_account_no OR t.security_code!=o.security_code OR t.side!=o.side OR t.trade_date!=o.trade_date`,
		`SELECT count(*) FROM orders o WHERE o.filled_quantity != COALESCE((SELECT SUM(t.quantity) FROM trades t WHERE t.order_id=o.id),0)`,
		`SELECT count(*) FROM orders o JOIN daily_quotes q ON q.security_code=o.security_code AND q.trade_date=o.trade_date WHERE o.price<q.low OR o.price>q.high OR o.quantity%100!=0`,
		`SELECT count(*) FROM positions WHERE sell_quantity>opening_quantity OR available_quantity!=quantity-buy_quantity`,
		`SELECT count(*) FROM (SELECT opening_quantity,lag(quantity) OVER(PARTITION BY fund_account_no,security_code ORDER BY trade_date) p FROM positions) WHERE p IS NOT NULL AND opening_quantity!=p`,
		`SELECT count(*) FROM (SELECT opening_balance,lag(closing_balance) OVER(PARTITION BY fund_account_no ORDER BY trade_date) p FROM daily_balances) WHERE p IS NOT NULL AND opening_balance!=p`,
		`SELECT count(*) FROM daily_balances b WHERE b.buy_amount!=COALESCE((SELECT sum(amount_cents) FROM trades t WHERE t.fund_account_no=b.fund_account_no AND t.trade_date=b.trade_date AND t.side='BUY'),0) OR b.sell_amount!=COALESCE((SELECT sum(amount_cents) FROM trades t WHERE t.fund_account_no=b.fund_account_no AND t.trade_date=b.trade_date AND t.side='SELL'),0) OR b.fees!=COALESCE((SELECT sum(fee_cents) FROM trades t WHERE t.fund_account_no=b.fund_account_no AND t.trade_date=b.trade_date),0)`,
	}
	for i, q := range checks {
		var n int
		if err := tx.QueryRow(q).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return fmt.Errorf("consistency check %d failed: %d rows", i, n)
		}
	}
	return nil
}

func comments(tx *sql.Tx) error {
	names := map[string]string{"roles": "岗位角色", "branches": "分公司（星澜证券为虚构券商）", "departments": "开户营业部", "employees": "员工登录身份", "employee_scopes": "员工管理范围（含生效日期）", "customers": "证券经纪客户（合成、脱敏）", "fund_accounts": "资金账户", "stock_accounts": "沪深证券账户", "securities": "模拟 A 股证券目录", "trading_calendar": "2026 交易日历（上交所休市安排）", "daily_quotes": "模拟日行情（价格及金额为分）", "orders": "证券交易委托", "trades": "证券成交及费用（分）", "cash_flows": "资金流水（有符号净发生额，分）", "positions": "日终持仓（T+1 可卖数量）", "daily_balances": "资金日结（分）", "schema_comments": "中文数据字典"}

	keys := make([]string, 0, len(names))
	for table := range names {
		keys = append(keys, table)
	}
	sort.Strings(keys)
	for _, table := range keys {
		n := names[table]
		if _, e := tx.Exec("INSERT INTO schema_comments VALUES (?,?,?)", table, "", n); e != nil {
			return e
		}
		rows, e := tx.Query("SELECT name FROM pragma_table_info(?)", table)
		if e != nil {
			return e
		}
		var cols []string
		for rows.Next() {
			var c string
			if e = rows.Scan(&c); e != nil {
				rows.Close()
				return e
			}
			cols = append(cols, c)
		}
		rows.Close()
		for _, c := range cols {
			v := columnNames[c]
			if table == "securities" && c == "code" {
				v = "证券代码（主键，六位字符串）"
			}
			if v == "" {
				v = strings.ReplaceAll(c, "_", " ") + "（演示字段）"
			}
			if _, e = tx.Exec("INSERT INTO schema_comments VALUES (?,?,?)", table, c, v); e != nil {
				return e
			}
		}
	}
	return nil
}
