#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Deterministic fictional sales generator. Standard library only, never overwrites."""
import argparse
import calendar
import datetime as dt
import json
import os
from pathlib import Path
import random
import sqlite3

ROOT = Path(__file__).resolve().parents[2]
TABLE_LABELS = dict(zip('regions provinces cities sales_offices departments employees employee_scopes customers customer_contacts sales_channels product_categories products price_lists product_prices warehouses inventory_balances sales_orders sales_order_items shipments shipment_items sales_returns sales_return_items payments payment_allocations refunds sales_targets schema_comments'.split(), '销售大区 省份 城市 销售分公司 部门 员工 员工数据范围 客户 客户联系人 销售渠道 商品分类 商品 地区价目表 商品价格 仓库 库存快照 销售订单 订单明细 发货单 发货明细 退货单 退货明细 回款 回款核销 退款 月度销售目标 结构注释'.split()))
COL_LABELS = {'id':'主键编号','code':'代码','name':'名称','region_id':'销售大区编号','province_id':'省份编号','city_id':'城市编号','office_id':'分公司编号','department_id':'部门编号','employee_id':'员工编号','employee_no':'工号','owner_employee_id':'归属销售员编号','scope_type':'授权范围类型','role':'业务岗位','active':'是否在职或启用','customer_id':'客户编号','customer_no':'客户编码','channel_id':'销售渠道编号','segment':'客户分层','acquired_date':'客户建立日期','phone':'虚构联系电话（不向业务查询开放）','email':'虚构邮箱（不向业务查询开放）','job_title':'联系人职务','category_id':'商品分类编号','sku':'商品编码','brand':'虚构品牌','list_price_cents':'商品目录价格（分）','standard_cost_cents':'标准成本（分）','price_list_id':'价目表编号','price_cents':'地区商品价格（分）','valid_from':'有效期开始','valid_to':'有效期结束','product_id':'商品编号','warehouse_id':'仓库编号','on_hand':'库存数量','reserved':'预留库存数量','snapshot_date':'库存快照日期','order_id':'订单编号','order_no':'订单编码','order_date':'订单日期','due_date':'应收款到期日期','status':'业务状态','total_cents':'订单折后总额（分，不含运费税额）','quantity':'数量','unit_price_cents':'成交单价快照（分）','unit_cost_cents':'成交单位成本快照（分）','discount_cents':'整行折扣金额（分）','net_cents':'整行折后成交金额（分）','shipment_id':'发货单编号','shipped_date':'发货日期','delivered_date':'签收日期','order_item_id':'订单明细编号','return_id':'退货单编号','return_date':'退货日期','reason':'退货原因','amount_cents':'业务金额（分）','payment_id':'回款编号','paid_date':'回款日期','method':'支付方式','refund_date':'退款日期','month':'目标月份（YYYY-MM）','target_cents':'销售员月度目标金额（分）','table_name':'物理表名','column_name':'物理字段名','comment_zh':'中文说明'}


def add_days(day, days, end):
    return min(day + dt.timedelta(days=days), end).isoformat()


def generate(path, orders=50000, seed=20261111, end_month='2026-10'):
    if orders < 240:
        raise ValueError('至少生成 240 张订单以覆盖 24 个月')
    last = dt.date.fromisoformat(end_month + '-01')
    end = last.replace(day=calendar.monthrange(last.year, last.month)[1])
    month_index = last.year * 12 + last.month - 1 - 23
    months = [dt.date((month_index+i)//12, (month_index+i)%12+1, 1) for i in range(24)]
    start = months[0]
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    fd = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    os.close(fd)
    db = None
    success = False
    try:
        db = sqlite3.connect(path)
        db.executescript(Path(__file__).with_name('schema.sqlite.sql').read_text())
        rng = random.Random(seed)
        def ins(table, *values):
            db.execute(f'INSERT INTO {table} VALUES ({",".join("?" for _ in values)})', values)
        with db:
            region_provinces = [('华东',['上海','江苏','浙江']),('华南',['广东','广西','海南']),('华北',['北京','天津','河北']),('华中',['湖北','湖南','河南']),('西南',['四川','重庆','云南']),('西北',['陕西','甘肃','新疆']),('东北',['辽宁','吉林'])]
            city_names = [('上海中心','上海浦东'),('南京','苏州'),('杭州','宁波'),('广州','深圳'),('南宁','桂林'),('海口','三亚'),('北京中心','北京海淀'),('天津中心','天津滨海'),('石家庄','唐山'),('武汉','宜昌'),('长沙','株洲'),('郑州','洛阳'),('成都','绵阳'),('重庆渝中','重庆江北'),('昆明','大理'),('西安','宝鸡'),('兰州','天水'),('乌鲁木齐','喀什'),('沈阳','大连'),('长春','吉林市')]
            cities, province, city = [], 0, 0
            for region, (name, provinces) in enumerate(region_provinces,1):
                ins('regions', region, f'R{region:02}', name)
                for pname in provinces:
                    province += 1
                    ins('provinces',province,region,pname)
                    for cname in city_names[province-1]:
                        city += 1
                        ins('cities',city,province,cname)
                        cities.append((city,region,cname))
            offices = []
            # 30 offices, all 7 regions represented; exact geographic mappings.
            chosen = [c for i,c in enumerate(cities) if i%4!=3]
            for office,(city,region,cname) in enumerate(chosen,1):
                ins('sales_offices',office,region,city,cname+'销售分公司')
                ins('departments',office,office,'销售业务部')
                ins('warehouses',office,region,office,cname+'仓')
                offices.append((office,region,city))
            scope_id=0
            def employee(eid, role, region=None, office=None):
                nonlocal scope_id
                ins('employees',eid,f'S{eid:04}',{'HQ':'总部分析员','REGION':'大区负责人','OFFICE':'分公司负责人','SALES':'销售员'}[role]+str(eid),office,office,region,role,1)
                scope_id+=1
                ins('employee_scopes',scope_id,eid,{'HQ':'ALL','REGION':'REGION','OFFICE':'OFFICE','SALES':'SELF'}[role],region if role=='REGION' else None,office if role=='OFFICE' else None)
            employee(1,'HQ')
            for r in range(1,8): employee(r+1,'REGION',r)
            for o,r,_ in offices: employee(8+o,'OFFICE',r,o)
            salespeople=[]
            for eid in range(39,301):
                o,r,city = offices[(eid-39)%30]
                employee(eid,'SALES',r,o)
                salespeople.append((eid,o,r,city))
            for i,name in enumerate(['直营','经销商','电商','政企采购'],1): ins('sales_channels',i,f'CH{i}',name)
            customers=[]
            for cid in range(1,3001):
                owner,o,r,city=salespeople[(cid-1)%len(salespeople)]
                channel=(cid-1)%4+1
                ins('customers',cid,f'C{cid:05}',f'虚构客户{cid:04}有限公司',city,r,o,owner,channel,['A','B','C'][cid%3],(start-dt.timedelta(days=rng.randint(30,720))).isoformat())
                ins('customer_contacts',cid,cid,f'演示联系人{cid}',f'DEMO-{cid:06}',f'contact{cid}@example.invalid','采购经理')
                customers.append((cid,owner,o,r,channel))
            cats=['冰箱','洗衣机','空调','电视','厨电','清洁电器','电脑','平板','手机','数码配件']
            for i,name in enumerate(cats,1): ins('product_categories',i,name)
            products=[]
            for pid in range(1,301):
                category=(pid-1)%10+1
                price=[350000,260000,420000,480000,120000,180000,650000,320000,450000,18000][category-1]+rng.randrange(0,100000,100)
                cost=price*rng.randint(48,78)//100
                ins('products',pid,f'SKU{pid:04}',f'{cats[category-1]}演示型号{pid:03}',category,['星河','云峰','海辰'][pid%3],price,cost,1)
                products.append((pid,price,cost))
            for r in range(1,8):
                ins('price_lists',r,r,f'大区{r}标准价',start.isoformat(),end.isoformat())
                for pid,price,_ in products: ins('product_prices',(r-1)*300+pid,r,pid,price*(95+r)//100)
            for o,_,_ in offices:
                for pid,_,_ in products:
                    stock=rng.randint(0,300)
                    ins('inventory_balances',(o-1)*300+pid,o,pid,stock,rng.randint(0,stock),end.isoformat())
            for mi,month in enumerate(months):
                for eid,o,r,_ in salespeople:
                    ins('sales_targets',mi*len(salespeople)+eid-38,month.strftime('%Y-%m'),r,o,eid,(18000000+eid%17*1000000)*(13 if month.month in (6,11,12) else 10)//10)
            item_id=ship_id=ship_item_id=ret_id=ret_item_id=pay_id=alloc_id=refund_id=0
            # Guarantee all months, then add seasonality and region/channel variation.
            weighted_months=[m for m in months for _ in range(18 if m.month in (6,11,12) else 10)]
            for oid in range(1,orders+1):
                month=months[oid-1] if oid<=24 else rng.choice(weighted_months)
                day=month.replace(day=rng.randint(1,calendar.monthrange(month.year,month.month)[1]))
                cid,owner,o,r,channel=rng.choice(customers)
                status=rng.choices(['pending','confirmed','partial_shipped','shipped','cancelled'],[6,10,12,66,6])[0]
                lines=[]
                # Region preferences and popular products make rankings interesting.
                for line in range(3):
                    pid,price,cost=products[((rng.randrange(80) if rng.random()<.65 else rng.randrange(300))+r*11+line)%300]
                    qty=rng.randint(1,6)
                    unit=price*(95+r)//100
                    discount=unit*qty*rng.choice([0,0,3,5,8,12])//100
                    item_id+=1
                    lines.append((item_id,pid,qty,unit,cost,discount,unit*qty-discount))
                total=sum(x[-1] for x in lines)
                ins('sales_orders',oid,f'RS-{day:%Y%m%d}-{oid:06}',cid,r,o,owner,channel,day.isoformat(),(day+dt.timedelta(days=30)).isoformat(),status,total)
                for iid,pid,q,u,c,d,n in lines: ins('sales_order_items',iid,oid,pid,q,u,c,d,n)
                if status in ('partial_shipped','shipped'):
                    batches=[[] , []]
                    for iid,pid,q,u,c,d,n in lines:
                        shipped=q if status=='shipped' else max(1,q//2)
                        if status=='partial_shipped' and iid!=lines[0][0]: shipped=0
                        first=(shipped+1)//2
                        if first: batches[0].append((iid,first))
                        if shipped-first: batches[1].append((iid,shipped-first))
                    for b,batch in enumerate(batches):
                        if not batch: continue
                        ship_id+=1
                        ins('shipments',ship_id,oid,o,add_days(day,1+b*2,end),add_days(day,4+b*2,end))
                        for iid,q in batch:
                            ship_item_id+=1
                            ins('shipment_items',ship_item_id,ship_id,iid,q)
                payment_ids=[]
                if status not in ('pending','cancelled'):
                    paid=total if rng.random()<.8 else total*rng.choice([30,50,70])//100
                    parts=[paid] if oid%3 else [paid//2,paid-paid//2]
                    for part in parts:
                        pay_id+=1; alloc_id+=1
                        ins('payments',pay_id,cid,r,o,owner,add_days(day,rng.randint(0,20),end),rng.choice(['bank','card','online']),part)
                        ins('payment_allocations',alloc_id,pay_id,oid,part)
                        payment_ids.append((pay_id,part))
                if status=='shipped' and rng.random()<.09:
                    ret_id+=1; ret_item_id+=1
                    rs=rng.choice(['requested','approved','received','received','rejected'])
                    ins('sales_returns',ret_id,oid,add_days(day,10,end),rng.choice(['质量问题','型号不符','客户退订']),rs)
                    iid,pid,q,u,c,d,n=lines[0]
                    amount=n//q
                    ins('sales_return_items',ret_item_id,ret_id,iid,1,amount)
                    if rs=='received' and payment_ids:
                        refund_id+=1
                        payment,part=payment_ids[0]
                        ins('refunds',refund_id,ret_id,payment,add_days(day,15,end),min(amount,part))
            for table,label in TABLE_LABELS.items():
                ins('schema_comments',table,'',label)
                for col in db.execute(f'PRAGMA table_info({table})').fetchall():
                    ins('schema_comments',table,col[1],COL_LABELS[col[1]])
        errors=db.execute('PRAGMA foreign_key_check').fetchall()
        if errors or db.execute('PRAGMA integrity_check').fetchone()[0]!='ok':
            raise ValueError(f'数据库完整性检查失败: {errors[:3]}')
        db.execute('ANALYZE')
        db.commit()
        success=True
        return {t:db.execute(f'SELECT COUNT(*) FROM {t}').fetchone()[0] for t in TABLE_LABELS}
    finally:
        if db: db.close()
        if not success:
            path.unlink(missing_ok=True)


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--db',default=str(ROOT/'data/regional_sales_demo.sqlite3'))
    parser.add_argument('--orders',type=int,default=50000)
    parser.add_argument('--seed',type=int,default=20261111)
    parser.add_argument('--end-month',default='2026-10',help='最后一个完整月份 YYYY-MM；向前生成24个月')
    args=parser.parse_args()
    print(json.dumps(generate(args.db,args.orders,args.seed,args.end_month),ensure_ascii=False,indent=2))

if __name__=='__main__': main()
