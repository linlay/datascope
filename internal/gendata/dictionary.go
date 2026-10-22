package gendata

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
)

var columnNames = map[string]string{
	"id": "内部主键", "code": "业务代码", "name": "名称", "description": "角色职责说明", "city": "所在城市", "address": "机构地址（虚构门牌）", "branch_code": "分公司代码，关联 branches.code", "department_code": "营业部代码，关联 departments.code", "emp_no": "员工工号（OAuth sub）", "role_id": "岗位角色编号", "title": "职务", "mobile": "手机号码（预先脱敏）", "hire_date": "入职日期", "status": "状态（员工 active/resigned；账户 active）", "employee_id": "员工编号（employees 主键，非自增）", "effective_date": "生效日期（含当天）", "expiry_date": "失效日期（不含当天），空为长期", "customer_no": "客户业务编号", "gender": "性别", "birth_date": "出生日期", "id_type": "证件类型", "id_no": "证件号码（预先脱敏）", "risk_level": "客户风险等级 C1–C5", "investor_type": "投资者类型（普通/专业）", "manager_id": "服务客户经理编号（允许跨营业部）", "open_date": "开户日期", "customer_type": "客户类型 INDIVIDUAL（个人）/CORPORATE（企业）", "fund_account_no": "资金账号", "currency": "结算币种 CNY", "exchange": "交易所 SH/SZ", "stock_account_no": "证券账号（沪 A+9 位，深 10 位）", "board": "板块 MAIN/STAR", "security_type": "证券类型 A_SHARE", "lot_size": "委托数量单位（股）", "min_buy_quantity": "最低买入委托股数", "trade_date": "交易日期 YYYY-MM-DD", "is_open": "是否开市（1 是，0 否）", "security_code": "证券代码，关联 securities.code", "prev_close": "前收盘价（分）", "open": "开盘价（分）", "high": "最高价（分）", "low": "最低价（分）", "close": "收盘价（分）", "volume": "成交量（股，模拟）", "amount": "行情成交金额（分，模拟）", "order_no": "委托业务编号", "order_time": "委托时间（中国时区 RFC3339）", "stock_account_id": "证券账户编号", "side": "买卖方向 BUY/SELL", "price": "委托或成交价格（分）", "quantity": "数量（股）", "filled_quantity": "委托累计成交股数", "channel": "渠道 APP/PC/COUNTER", "trade_no": "成交业务编号", "order_id": "所属委托编号", "trade_time": "成交时间（中国时区 RFC3339）", "amount_cents": "成交金额或资金净发生额（分，流水带正负号）", "commission_cents": "佣金（分，按委托计费后分摊）", "stamp_tax_cents": "卖出印花税（分）", "transfer_fee_cents": "过户费（分）", "fee_cents": "费用合计（分）", "flow_type": "流水类型 DEPOSIT/WITHDRAWAL/BUY/SELL", "trade_id": "对应成交编号（转入转出为空）", "balance_after": "流水后资金余额（分）", "opening_quantity": "期初持仓股数", "buy_quantity": "当日买入股数", "sell_quantity": "当日卖出股数（不得超过期初）", "available_quantity": "当日收盘可卖股数（扣除当日买入）", "cost_cents": "剩余持仓成本（分，含买入费用）", "close_price": "当日收盘价（分）", "market_value_cents": "持仓市值（分）", "opening_balance": "期初资金余额（分）", "transfer_in": "当日转入金额（分）", "transfer_out": "当日转出金额（分）", "buy_amount": "买入成交总金额（分，不含费用）", "sell_amount": "卖出成交总金额（分，不含费用）", "fees": "当日交易费用（分）", "closing_balance": "期末资金余额（分）", "available_balance": "可用资金（分）", "withdrawable_balance": "可取资金（分，保守扣除当日卖出金额）", "table_name": "表名", "column_name": "字段名（空为表注释）", "comment_zh": "中文注释",
}

// WriteDictionary exports the schema and Chinese comments from the generated database.
func WriteDictionary(path, dest string) error {
	db, e := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if e != nil {
		return e
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	rows, e := db.Query("SELECT table_name,comment_zh FROM schema_comments WHERE column_name='' ORDER BY table_name")
	if e != nil {
		return e
	}
	type table struct{ name, comment string }
	var tables []table
	for rows.Next() {
		var t table
		if e = rows.Scan(&t.name, &t.comment); e != nil {
			rows.Close()
			return e
		}
		tables = append(tables, t)
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return e
	}
	rows.Close()
	var out strings.Builder
	out.WriteString("# 证券经纪业务数据字典\n\n由 Go 生成器导出。全部为合成数据，价格与金额均为整数分；日期为 YYYY-MM-DD，交易时间使用中国时区 RFC3339。证券代码为格式演示，非真实行情。\n\n")
	for _, t := range tables {
		var n int64
		if e = db.QueryRow("SELECT count(*) FROM " + t.name).Scan(&n); e != nil {
			return e
		}
		fmt.Fprintf(&out, "## %s — %s\n\n默认数据量：%d 行。\n\n| 字段 | 类型 | 必填 | 主键 | 中文说明 |\n| --- | --- | --- | --- | --- |\n", t.name, t.comment, n)
		r, e := db.Query(`SELECT p.name,p.type,p."notnull",p.pk,c.comment_zh FROM pragma_table_info(?) p LEFT JOIN schema_comments c ON c.table_name=? AND c.column_name=p.name ORDER BY p.cid`, t.name, t.name)
		if e != nil {
			return e
		}
		for r.Next() {
			var name, typ, comment string
			var notnull, pk int
			if e = r.Scan(&name, &typ, &notnull, &pk, &comment); e != nil {
				r.Close()
				return e
			}
			required := "否"
			if notnull != 0 || pk != 0 {
				required = "是"
			}
			key := ""
			if pk != 0 {
				key = "是"
			}
			fmt.Fprintf(&out, "| %s | %s | %s | %s | %s |\n", name, typ, required, key, comment)
		}
		if e = r.Err(); e != nil {
			r.Close()
			return e
		}
		r.Close()
		out.WriteString("\n")
	}
	return os.WriteFile(dest, []byte(strings.TrimRight(out.String(), "\n")+"\n"), 0644)
}
