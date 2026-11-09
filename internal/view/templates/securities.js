// Securities-only screens. All reads go through the host's permission-aware query tool.
(() => {
 'use strict';
 const ds = window.datascope;
 const queries = JSON.parse(document.getElementById('securities-queries').textContent);
 const labels = {customer_no:'客户编号',name:'客户姓名',customer_type:'客户类型',risk_level:'风险等级',investor_type:'投资者类型',open_date:'开户日期',status:'状态',account_count:'资金账户数',fund_account_no:'资金账号',currency:'币种',department_code:'营业部编号',manager_id:'客户经理编号',security_code:'证券代码',quantity:'数量',available_quantity:'可用数量',cost_cents:'持仓成本（元）',market_value_cents:'持仓市值（元）',trade_no:'成交编号',trade_date:'交易日期',side:'方向',price:'价格（元）',amount_cents:'金额（元）',fee_cents:'费用（元）',flow_type:'流水类型',balance_after:'流水后余额（元）'};
 const values = {INDIVIDUAL:'个人',CORPORATE:'机构',active:'正常',frozen:'冻结',closed:'已关闭',BUY:'买入',SELL:'卖出',DEPOSIT:'入金',WITHDRAWAL:'出金'};
 const monetary = new Set(['cost_cents','market_value_cents','price','amount_cents','fee_cents','balance_after']);
 const money = value => value === null || value === undefined ? '—' : ds.money(value);
 const query = (key, parameters = {}, max_rows = 200) => {
  // Send only parameters used by this statement; identifiers never come from input.
  const binds = {}; for (const name of new Set(queries[key].match(/:arg_[a-z_]+/g) || [])) binds[name.slice(1)] = parameters[name.slice(1)];
  return ds.callTool('query', {sql:queries[key],parameters:binds,max_rows});
 };
 const text = (target, value) => { target.textContent = value; };
 function showTable(target, result, actions) {
  ds.table(target, {...result,columns:result.columns.map(k=>labels[k]||k),rows:result.rows.map(row=>{
   const shown={...row};for(const k of result.columns) shown[labels[k]||k]=monetary.has(k)?money(row[k]):values[row[k]]||row[k];return shown;
  })}, actions);
 }
 function field(target, label, value) {
  const item=ds.el('div');item.append(ds.el('span',label),ds.el('strong',values[value]||String(value??'—')));target.append(item);
 }
 function metric(target, label, value) {
  const card=ds.el('div',undefined,'metric');card.append(ds.el('span',label),ds.el('strong',value));target.append(card);
 }
 const isoDate = value => /^\d{4}-\d{2}-\d{2}$/.test(value) && !Number.isNaN(Date.parse(value));
 function customerPanel(root) {
  let generation=0;
  return async (customerNo) => {
   const stamp=++generation;root.hidden=false;root.replaceChildren();
   const heading=ds.el('div',undefined,'top'), title=ds.el('h2','客户详情'), close=ds.button('收起详情',()=>{generation++;root.hidden=true;root.replaceChildren();});heading.append(title,close);root.append(heading);
   const status=ds.el('div');status.setAttribute('role','status');root.append(status);ds.notice(status,'正在读取客户资料…');
   try {
    const params={arg_customer:customerNo};
    const customer=await query('customer',params);if(stamp!==generation)return;
    const c=customer.rows[0];if(!c){ds.notice(status,'没有可见的客户记录。');return;}
    title.textContent=`${c.name} · 客户详情`;
    const details=ds.el('div',undefined,'details');
    for(const [label,key] of [['客户编号','customer_no'],['客户姓名','name'],['客户类型','customer_type'],['手机号','mobile'],['风险等级','risk_level'],['投资者类型','investor_type'],['开户日期','open_date'],['客户状态','status']])field(details,label,c[key]);
    root.append(details);ds.notice(status,'');
    const accountTitle=ds.el('h2','资金账户'),accounts=ds.el('div',undefined,'scroll');root.append(accountTitle,accounts);
    const dates=await query('customer_dates',params);if(stamp!==generation)return;
    const date=ds.el('input');date.type='date';date.setAttribute('aria-label','客户资产日期');date.value=dates.rows[0]?.last_date||'';
    const toolbar=ds.el('div',undefined,'toolbar'), dateLabel=ds.el('label','资产与记录日期');dateLabel.append(date);const refresh=ds.button('刷新客户数据',()=>loadDate());toolbar.append(dateLabel,refresh);root.append(toolbar);
    const metrics=ds.el('div',undefined,'metrics');root.append(metrics);
    const tabs=ds.el('div',undefined,'toolbar tabs');tabs.setAttribute('role','tablist');tabs.setAttribute('aria-label','客户记录');root.append(tabs);
    const recordStatus=ds.el('div'),records=ds.el('div',undefined,'scroll'),pager=ds.el('div',undefined,'toolbar pager'),pageInfo=ds.el('span',undefined,'muted');recordStatus.setAttribute('role','status');
    let tab='positions',page=0,recordStamp=0,dateStamp=0;
    const previous=ds.button('上一页',()=>{page--;loadRecords();}),next=ds.button('下一页',()=>{page++;loadRecords();});pager.append(previous,pageInfo,next);root.append(recordStatus,records,pager);
    const tabButtons=[];for(const [key,label] of [['positions','持仓'],['customer_trades','成交'],['flows','资金流水']]){const button=ds.button(label,()=>{tab=key;page=0;loadRecords();});button.setAttribute('role','tab');tabButtons.push([button,key]);tabs.append(button);}
    const rows=await query('accounts',params);if(stamp!==generation)return;showTable(accounts,rows);
    async function loadRecords() {
     const ticket=++recordStamp,selected=tab;records.replaceChildren();previous.disabled=next.disabled=true;pageInfo.textContent='';
     tabButtons.forEach(([b,key])=>b.setAttribute('aria-selected',String(key===selected)));
     if(!isoDate(date.value)){ds.notice(recordStatus,'请选择有效的记录日期。',true);return;}
     ds.notice(recordStatus,'正在读取记录…');
     try {
      const bind={...params,arg_date:date.value,arg_limit:20,arg_offset:page*20};
      const [count,result]=await Promise.all([query(selected+'_count',bind),query(selected,bind)]);
      if(stamp!==generation||ticket!==recordStamp)return;
      showTable(records,result);const total=Number(count.rows[0]?.total||0);pageInfo.textContent=`共 ${total} 条 · 第 ${page+1} / ${Math.max(1,Math.ceil(total/20))} 页`;previous.disabled=page===0;next.disabled=(page+1)*20>=total;ds.notice(recordStatus,'');
     }catch(e){if(stamp===generation&&ticket===recordStamp)ds.notice(recordStatus,e.message,true);}
    }
    async function loadDate() {
     const ticket=++dateStamp;++recordStamp;metrics.replaceChildren();records.replaceChildren();previous.disabled=next.disabled=true;refresh.disabled=true;page=0;
     if(!isoDate(date.value)){ds.notice(status,'没有可见的资金账期，请选择有效日期。',true);refresh.disabled=false;return;}
     ds.notice(status,'正在汇总客户资产…');
     try {
      const bind={...params,arg_date:date.value};
      const [cash,holdings,activity]=await Promise.all([query('customer_cash',bind),query('customer_holdings',bind),query('customer_activity',bind)]);
      if(stamp!==generation||ticket!==dateStamp)return;
      const b=cash.rows[0]||{},h=holdings.rows[0]||{},a=activity.rows[0]||{};
      const total=b.cash==null&&h.holdings==null?null:Number(b.cash||0)+Number(h.holdings||0);
      for(const [label,value] of [['总资产（元）',money(total)],['资金余额（元）',money(b.cash)],['持仓市值（元）',money(h.holdings)],['可用资金（元）',money(b.available)],['当日成交额（元）',money(a.turnover)],['当日成交笔数',String(a.trade_count??'—')]])metric(metrics,label,value);
      ds.notice(status,'');await loadRecords();
     }catch(e){if(stamp===generation&&ticket===dateStamp)ds.notice(status,e.message,true);}
     finally{if(stamp===generation&&ticket===dateStamp)refresh.disabled=false;}
    }
    date.onchange=()=>loadDate();await loadDate();
   }catch(e){if(stamp===generation)ds.notice(status,e.message,true);}
  };
 }
 function start(view) {
  const byID=id=>document.getElementById(id),detail=customerPanel(byID('customer-detail'));
  let epoch=0;
  const detailButton=(td,row)=>td.append(ds.button('客户详情',()=>{detail(row.customer_no);byID('customer-detail').scrollIntoView({behavior:'smooth',block:'start'});}));
  const form=byID('filters'),status=byID('status'),submit=form.querySelector('button');
  // Sandboxed views intentionally have no allow-forms capability. Handle clicks
  // and Enter directly instead of attempting native form submission.
  let submitForm=()=>{};submit.type='button';submit.onclick=()=>submitForm();
  form.onkeydown=e=>{if(e.key==='Enter'){e.preventDefault();submitForm();}};
  const customers=view==='securities_customers',rankings=view==='securities_rankings';
  if(customers){
   let page=0,filter={arg_keyword:'',arg_like:'%',arg_risk:'',arg_type:''};
   const previous=byID('previous'),next=byID('next');
   async function load(){
    const ticket=++epoch;submit.disabled=previous.disabled=next.disabled=true;byID('results').replaceChildren();byID('page-info').textContent='';ds.notice(status,'正在查询客户…');
    try{const bind={...filter,arg_limit:20,arg_offset:page*20};const [count,result]=await Promise.all([query('customer_count',bind),query('customers',bind)]);if(ticket!==epoch)return;
     showTable(byID('results'),result,detailButton);const total=Number(count.rows[0]?.total||0);text(byID('page-info'),`当前可见客户 ${total} 位 · 第 ${page+1} / ${Math.max(1,Math.ceil(total/20))} 页`);previous.disabled=page===0;next.disabled=(page+1)*20>=total;ds.notice(status,'');
    }catch(e){if(ticket===epoch)ds.notice(status,e.message,true);}finally{if(ticket===epoch)submit.disabled=false;}
   }
   submitForm=()=>{page=0;filter={arg_keyword:byID('keyword').value.trim(),arg_like:`%${byID('keyword').value.trim()}%`,arg_risk:byID('risk').value,arg_type:byID('type').value};load();};previous.onclick=()=>{page--;load();};next.onclick=()=>{page++;load();};
   ds.onData(data=>{if(data.input?.keyword!==undefined)byID('keyword').value=String(data.input.keyword);submitForm();});
  }else if(rankings){
   const names={assets:'总资产',cash:'资金余额',holdings:'持仓市值',turnover:'成交额',trade_count:'成交笔数',net_deposit:'净入金',fees:'手续费'};
   function period(){const value=byID('metric').value;const historical=['turnover','trade_count','net_deposit','fees'].includes(value);byID('snapshot-label').hidden=historical;byID('start-label').hidden=byID('end-label').hidden=!historical;byID('snapshot').required=!historical;byID('start').required=byID('end').required=historical;return historical;}
   async function load(){const ticket=++epoch;submit.disabled=true;byID('results').replaceChildren();ds.notice(status,'正在计算客户排名…');
    try{const selected=byID('metric').value,historical=period(),bind={arg_date:byID('snapshot').value,arg_start:byID('start').value,arg_end:byID('end').value,arg_limit:Number(byID('top').value)};
     if(historical?(!isoDate(bind.arg_start)||!isoDate(bind.arg_end)||bind.arg_start>bind.arg_end):!isoDate(bind.arg_date))throw new Error('请选择有效日期，开始日期不能晚于结束日期。');
     text(byID('basis'),`当前身份可见客户 · ${names[selected]}降序 Top ${bind.arg_limit} · ${historical?bind.arg_start+' 至 '+bind.arg_end:bind.arg_date}${selected==='assets'?' · 总资产 = 当日资金余额 + 当日持仓市值':''} · 金额单位：元`);
     const result=await query(selected,bind);if(ticket!==epoch)return;
     const rows=result.rows.map((r,i)=>({...r,'排名':i+1,'客户编号':r.customer_no,'客户姓名':r.name,[names[selected]+(selected==='trade_count'?'（笔）':'（元）')]:selected==='trade_count'?r.metric:money(r.metric)}));
     ds.table(byID('results'),{...result,rows,columns:['排名','客户编号','客户姓名',names[selected]+(selected==='trade_count'?'（笔）':'（元）')]},detailButton);ds.notice(status,'');
    }catch(e){if(ticket===epoch)ds.notice(status,e.message,true);}finally{if(ticket===epoch)submit.disabled=false;}
   }
   byID('metric').onchange=()=>{period();submitForm();};submitForm=()=>{load();};
   ds.onData(async data=>{const ticket=++epoch;submit.disabled=true;ds.notice(status,'正在读取可见数据日期…');
    try{const result=await query('dates');if(ticket!==epoch)return;const dates=result.rows[0]||{},input=data.input||{};byID('snapshot').value=input.date||dates.last_date||'';byID('start').value=input.start_date||dates.first_date||'';byID('end').value=input.end_date||dates.last_date||'';if(Object.hasOwn(names,input.metric))byID('metric').value=input.metric;await load();}
    catch(e){if(ticket===epoch){submit.disabled=false;ds.notice(status,e.message,true);}}
   });
  }else{
   submitForm=()=>{if(!form.reportValidity())return;ds.notice(status,'');detail(byID('customer-number').value.trim());};
   ds.onData(data=>{const no=data.input?.customer_no||data.result?.rows?.[0]?.customer_no;if(no){byID('customer-number').value=String(no);submitForm();}else ds.notice(status,'输入客户编号，或从客户列表、客户排名中查看客户详情。');});
  }
 }
 window.securitiesViews={start};
})();
