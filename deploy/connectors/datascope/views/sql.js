(() => {
  'use strict';
  const NAME = /^[a-z][a-z0-9_]{0,31}$/;
  const PLACEHOLDER = /^\{\{\s*([a-z][a-z0-9_]{0,31})\s*\}\}/;
  const INSIDE = /\{\{\s*([a-z][a-z0-9_]{0,31})\s*\}\}/g;

  function optionValue(o) { return o && typeof o === 'object' ? o.value : o; }
  function optionLabel(o) { return o && typeof o === 'object' && o.label ? o.label : String(optionValue(o)); }
  function signed(n) { return n < 0 ? `(${n})` : String(n); }

  // literal turns one variable value (blank means the default) into SQL text.
  // Quoting is for correctness only: the server still parses and permission-
  // rewrites the final statement, so this is not the security boundary.
  function literal(def, raw) {
    const value = raw === undefined || raw === null || raw === '' ? def.default : raw;
    if (Array.isArray(def.options) && def.type !== 'boolean' && !def.options.some(o => String(optionValue(o)) === String(value))) {
      throw new Error('不在可选值中');
    }
    const text = String(value).trim();
    switch (def.type) {
      case 'string':
        if (String(value).includes('\u0000')) throw new Error('不能包含 NUL 字符');
        return `'${String(value).replace(/'/g, "''")}'`;
      case 'integer': {
        const n = Number(text);
        if (!/^[+-]?\d+$/.test(text) || !Number.isSafeInteger(n)) throw new Error('应为整数');
        return signed(n);
      }
      case 'number': {
        const n = Number(text);
        if (!/^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$/.test(text) || !Number.isFinite(n)) throw new Error('应为数字');
        return signed(n);
      }
      case 'date': {
        const d = new Date(`${text}T00:00:00Z`);
        if (value !== text || !/^\d{4}-\d{2}-\d{2}$/.test(text) || Number.isNaN(d.getTime()) || d.toISOString().slice(0, 10) !== text) throw new Error('应为 YYYY-MM-DD 日期');
        return `'${text}'`;
      }
      case 'boolean':
        if (value === true || value === 'true') return '1';
        if (value === false || value === 'false') return '0';
        throw new Error('应为 true 或 false');
      default:
        throw new Error(`不支持的类型 ${def.type}`);
    }
  }

  // fill replaces bare {{name}} placeholders outside string literals, quoted
  // identifiers and comments. Any placeholder inside them is an error,
  // because the view adds quotes itself; an unknown one outside is an error too.
  function fill(sql, variables, values = {}) {
    const vars = variables && typeof variables === 'object' ? variables : {};
    const used = new Set(), fieldErrors = {};
    let out = '', i = 0;
    const guard = segment => {
      for (const m of segment.matchAll(INSIDE)) {
        throw new Error(`变量 {{${m[1]}}} 不能写在引号或注释里，视图会按类型自动加引号`);
      }
      out += segment;
    };
    const until = (end, from, doubled) => {
      let j = from;
      for (;;) {
        const k = sql.indexOf(end, j);
        if (k < 0) return sql.length;
        if (doubled && sql.startsWith(end, k + end.length)) { j = k + end.length * 2; continue; }
        return k + end.length;
      }
    };
    while (i < sql.length) {
      const c = sql[i], rest = sql.slice(i);
      let end = -1;
      if (rest.startsWith('--')) { const k = sql.indexOf('\n', i); end = k < 0 ? sql.length : k; }
      else if (rest.startsWith('/*')) { const k = sql.indexOf('*/', i + 2); end = k < 0 ? sql.length : k + 2; }
      else if (c === "'" || c === '"' || c === '`') end = until(c, i + 1, true);
      else if (c === '[') end = until(']', i + 1, false);
      if (end >= 0) { guard(sql.slice(i, end)); i = end; continue; }
      if (rest.startsWith('{{')) {
        const m = PLACEHOLDER.exec(rest);
        if (!m) throw new Error(`无法识别的占位符：${rest.slice(0, 24)}`);
        const name = m[1];
        if (!Object.hasOwn(vars, name)) throw new Error(`SQL 使用了未声明的变量 {{${name}}}`);
        used.add(name);
        try { out += literal(vars[name], Object.hasOwn(values, name) ? values[name] : undefined); } catch (e) { fieldErrors[name] = e.message; }
        i += m[0].length;
        continue;
      }
      out += c; i++;
    }
    return { sql: out, used, fieldErrors };
  }

  // validate checks the definitions themselves, so a bad default is reported
  // before any query instead of looking like a user input error.
  function validate(doc) {
    if (!doc || typeof doc !== 'object' || typeof doc.sql !== 'string' || !doc.sql.trim()) return '视图数据缺少 sql。';
    if (!doc.variables || typeof doc.variables !== 'object' || Array.isArray(doc.variables)) return '视图数据缺少合法的 variables 对象。';
    const vars = doc.variables;
    for (const [name, def] of Object.entries(vars)) {
      if (!NAME.test(name)) return `变量名 ${name} 不合法。`;
      if (!def || typeof def !== 'object' || def.default === undefined) return `变量 ${name} 缺少默认值。`;
      const defaultType = { string: 'string', date: 'string', integer: 'number', number: 'number', boolean: 'boolean' }[def.type];
      if (!defaultType || typeof def.default !== defaultType) return `变量 ${name} 的默认值类型无效。`;
      try { literal(def, undefined); } catch (e) { return `变量 ${name} 的默认值无效：${e.message}`; }
    }
    return '';
  }

  const api = { fill, literal, validate };
  if (typeof module === 'object' && module.exports) { module.exports = api; return; }

  const app = document.getElementById('app');
  const ROWS = [50, 200, 500, 1000];
  const known = {
    connector_auth_required: '请先在连接器页面完成 DataScope 授权。',
    connector_auth_expired: 'DataScope 授权已变化，请在连接器页面重新检查授权。',
    connector_configuration_required: '请先完成 DataScope 连接器配置。',
    connector_busy: 'DataScope 连接器正忙，请稍后重试。',
    connector_unavailable: 'DataScope 连接器不可用，请确认数据库 MCP 已启动并开放 query 工具。',
    invocation_timeout: '查询超时，请缩小查询范围后重试。',
    view_query_unavailable: '当前视图不可查询，请在在线 Chat 中打开。',
    view_query_run_not_found: '找不到该视图所属的运行，请在当前 Chat 中重新生成视图。',
    view_query_params_invalid: '查询参数无效，SQL 可能过长。',
  };
  // The platform serializes calls per connector and rejects overlapping ones,
  // so several views auto-running together collide. The host may hand over the
  // raw code or its localized text.
  const BUSY = /connector_busy|正在初始化、授权或更新|being prepared, authorized, or updated/;
  const RETRIES = 4;
  const state = { doc: null, key: '', queryable: false, values: {}, maxRows: 200, busy: false, stamp: 0, result: null, error: '', fieldErrors: {}, sent: '', executedAt: 0, showSql: false };
  let form, results, sqlToggle, sqlPane;

  function el(tag, cls, text) { const n = document.createElement(tag); if (cls) n.className = cls; if (text !== undefined) n.textContent = text; return n; }
  function button(text, fn, cls = '') { const b = el('button', cls, text); b.type = 'button'; b.addEventListener('click', fn); return b; }
  function notice(parent, text, bad = false) { const n = el('p', 'notice' + (bad ? ' error' : ''), text); n.setAttribute('role', bad ? 'alert' : 'status'); parent.append(n); }
  function message(e) { const text = typeof e === 'string' ? e : e?.message || ''; return known[text] || text || '查询失败，请重试。'; }
  function vars() { return state.doc?.variables && typeof state.doc.variables === 'object' ? state.doc.variables : {}; }
  function usedNames() { try { return fill(state.doc.sql, vars()).used; } catch { return new Set(Object.keys(vars())); } }
  function pause(ms) { return new Promise(resolve => setTimeout(resolve, ms)); }

  async function query(stamp, params) {
    for (let attempt = 0; ; attempt++) {
      try { return await window.viewHost.query('run', params); }
      catch (e) {
        if (attempt >= RETRIES || stamp !== state.stamp || !BUSY.test(e?.message || '')) throw e;
        await pause(250 * (attempt + 1) + Math.random() * 250);
        if (stamp !== state.stamp) throw e;
      }
    }
  }

  async function run() {
    if (!state.queryable || !state.doc) return;
    const stamp = ++state.stamp;
    state.busy = false;
    let filled;
    try { filled = fill(state.doc.sql, vars(), state.values); }
    catch (e) { state.error = e.message; state.fieldErrors = {}; state.result = null; renderForm(); renderResults(); return; }
    state.fieldErrors = filled.fieldErrors;
    if (Object.keys(filled.fieldErrors).length) { state.error = '请修正标红的变量后再查询。'; state.result = null; renderForm(); renderResults(); return; }
    state.busy = true; state.error = ''; state.sent = filled.sql;
    renderForm(); renderResults();
    try {
      const reply = await query(stamp, { sql: filled.sql, max_rows: state.maxRows });
      if (stamp !== state.stamp) return;
      state.result = reply.data; state.executedAt = reply.executedAt || Date.now();
    } catch (e) {
      if (stamp !== state.stamp || e?.message === 'view_query_superseded') return;
      state.error = message(e); state.result = null;
    } finally {
      if (stamp === state.stamp) { state.busy = false; renderForm(); renderResults(); }
    }
  }

  function control(name, def) {
    const raw = Object.hasOwn(state.values, name) ? state.values[name] : undefined;
    if (def.type === 'boolean') {
      const box = el('input'); box.type = 'checkbox';
      box.checked = raw === undefined ? def.default === true || def.default === 'true' : raw === true;
      box.addEventListener('change', () => { state.values[name] = box.checked; run(); });
      return box;
    }
    if (Array.isArray(def.options)) {
      const select = el('select');
      const current = String(raw === undefined || raw === '' ? def.default : raw);
      def.options.forEach(o => { const opt = el('option', '', optionLabel(o)); opt.value = String(optionValue(o)); opt.selected = opt.value === current; select.append(opt); });
      select.addEventListener('change', () => { state.values[name] = select.value; run(); });
      return select;
    }
    const input = el('input');
    input.type = def.type === 'date' ? 'date' : 'text';
    if (def.type === 'integer' || def.type === 'number') input.inputMode = 'decimal';
    // Show the value the query used: date inputs never display a placeholder.
    input.value = String(raw === undefined ? def.default : raw);
    input.placeholder = String(def.default);
    input.addEventListener('input', () => { state.values[name] = input.value; });
    // The host sandbox has no allow-forms, so Enter is wired directly.
    input.addEventListener('keydown', e => { if (e.key === 'Enter' && !e.isComposing) { e.preventDefault(); run(); } });
    return input;
  }

  // Controls live in a plain container: the host frame is sandboxed without
  // allow-forms, so a <form> submit would be dropped before any listener runs.
  function renderForm() {
    form.replaceChildren();
    const used = usedNames();
    for (const [name, def] of Object.entries(vars())) {
      const label = el('label', def.type === 'boolean' ? 'var check' : 'var');
      if (!used.has(name)) { label.classList.add('unused'); label.title = 'SQL 中未使用该变量'; }
      const caption = el('span', '', def.label || name);
      if (def.description) caption.title = def.description;
      const field = control(name, def);
      field.name = name;
      if (state.fieldErrors[name]) { field.setAttribute('aria-invalid', 'true'); caption.textContent += `（${state.fieldErrors[name]}）`; caption.classList.add('error'); }
      if (def.type === 'boolean') label.append(field, caption); else label.append(caption, field);
      form.append(label);
    }
    const actions = el('div', 'actions');
    const submit = button(state.busy ? '查询中…' : '查询', () => run(), 'primary');
    submit.disabled = !state.queryable || state.busy;
    const reset = button('恢复默认', () => { state.values = {}; state.fieldErrors = {}; run(); });
    reset.disabled = !state.queryable || !Object.keys(state.values).length;
    const size = el('select'); size.setAttribute('aria-label', '最多返回行数');
    [...new Set([...ROWS, state.maxRows])].sort((a, b) => a - b).forEach(n => { const o = el('option', '', `最多 ${n} 行`); o.value = n; o.selected = n === state.maxRows; size.append(o); });
    size.disabled = !state.queryable;
    // Row limit applies at once, like any other select.
    size.addEventListener('change', () => { state.maxRows = Number(size.value); run(); });
    actions.append(submit, reset, size);
    form.append(actions);
  }

  function renderSql() {
    sqlToggle.textContent = state.sent ? '执行的 SQL' : 'SQL 模板';
    sqlToggle.setAttribute('aria-expanded', String(state.showSql));
    sqlToggle.className = 'ghost' + (state.showSql ? ' on' : '');
    sqlPane.hidden = !state.showSql;
    sqlPane.textContent = state.sent || state.doc.sql;
  }

  function cell(value) {
    const td = el('td');
    if (value === null || value === undefined) { td.className = 'null'; td.textContent = 'NULL'; }
    else if (typeof value === 'number') { td.className = 'num'; td.textContent = String(value); }
    else if (typeof value === 'object') td.textContent = JSON.stringify(value);
    else td.textContent = String(value);
    return td;
  }

  function renderResults() {
    renderSql();
    results.replaceChildren();
    if (!state.queryable) notice(results, '该视图需要在在线 Chat 中查询，当前仅显示 SQL 与变量。');
    if (state.error) notice(results, state.error, true);
    const data = state.result;
    if (!data) { if (state.busy) notice(results, '查询中…'); return; }
    if (typeof data.text === 'string' && !Array.isArray(data.rows)) { notice(results, data.text); return; }
    const rows = Array.isArray(data.rows) ? data.rows : [];
    const columns = Array.isArray(data.columns) && data.columns.length ? data.columns : Object.keys(rows[0] || {});
    if (!rows.length) { results.append(el('div', 'empty', '没有符合条件的数据')); }
    else {
      const wrap = el('div', 'scroll'), table = el('table'), head = el('tr'), body = el('tbody');
      columns.forEach(c => head.append(el('th', '', c)));
      rows.forEach(r => { const tr = el('tr'); columns.forEach(c => tr.append(cell(r?.[c]))); body.append(tr); });
      const thead = el('thead'); thead.append(head); table.append(thead, body); wrap.append(table); results.append(wrap);
    }
    let meta = `${data.row_count ?? rows.length} 行`;
    if (data.truncated) meta += ` · 已截断，仅显示前 ${rows.length} 行`;
    if (state.executedAt) meta += ` · ${new Date(state.executedAt).toLocaleString(document.documentElement.lang || 'zh-CN', { hour12: false })}`;
    results.append(el('p', 'meta footer', meta));
  }

  function render() {
    app.replaceChildren();
    const problem = validate(state.doc);
    const header = el('div', 'header');
    const title = el('h1', '', state.doc?.title || 'SQL 查询'); title.title = title.textContent;
    header.append(title);
    app.append(header);
    if (problem) { notice(app, problem, true); return; }
    sqlToggle = button('', () => { state.showSql = !state.showSql; renderSql(); });
    header.append(sqlToggle);
    sqlPane = el('pre', 'sql');
    form = el('div', 'controls');
    results = el('section'); results.setAttribute('aria-live', 'polite');
    app.append(sqlPane, form, results);
    renderForm(); renderResults();
  }

  if (!window.viewHost?.onData) { notice(app, '请通过 Platform VIEW 宿主打开此视图。', true); return; }
  window.viewHost.onData((payload, view, context = {}) => {
    const key = JSON.stringify(payload);
    const queryable = Boolean(context.queryable);
    if (key === state.key && queryable === state.queryable) return; // locale/theme refresh only
    const fresh = key !== state.key;
    state.stamp++; // Invalidate pending results when the payload or queryability changes.
    state.busy = false;
    state.queryable = queryable;
    if (fresh) {
      state.key = key; state.doc = payload; state.values = {}; state.showSql = false; state.result = null; state.error = ''; state.sent = ''; state.fieldErrors = {};
      const rows = Number(payload?.maxRows);
      state.maxRows = Number.isInteger(rows) && rows >= 1 && rows <= 1000 ? rows : 200;
    }
    render();
    if (queryable && !validate(state.doc)) run(); // defaults run at once, including when the host becomes queryable
  });
})();
