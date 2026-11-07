import { useState } from 'react';
import { Alert, Button, Card, Space, Typography } from 'antd';
import { managementCall, type Database } from '../api';
import { CodeEditor } from './CodeEditor';
import { useI18n } from '../ui/preferences';

export function SQLiteSchemaEditor({ db, onSaved }: { db: Database; onSaved: () => void }) {
 const t = useI18n();
 const [sql, setSQL] = useState('');
 const [busy, setBusy] = useState(false);
 const [error, setError] = useState<string>();
 const [done, setDone] = useState(false);
 const execute = async () => {
  setBusy(true); setError(undefined); setDone(false);
  try {
   await managementCall('edit_sqlite_schema', { database_id: db.id, sql });
   setSQL(''); setDone(true);
   try { await managementCall('discover_database', { database_id: db.id }); }
   catch(e) { setError(t('结构已修改，但扫描失败：') + (e as Error).message); }
   onSaved();
  } catch(e) { setError((e as Error).message); }
  finally { setBusy(false); }
 };
 return <Card title={t('SQLite 表结构')}>
  <Space orientation="vertical" style={{width:'100%'}}>
   <Typography.Text type="secondary">{t('每次执行一条 CREATE / ALTER / DROP TABLE 或 CREATE / DROP INDEX。删除表或字段会删除对应数据，请执行前核对。')}</Typography.Text>
   {error && <Alert type="error" title={error}/>}
   {done && <Alert type="success" title={t('结构已修改。请在查询配置的结构侦查中审核并应用变更。')}/>}
   <CodeEditor language="sql" value={sql} onChange={setSQL} height="180px" />
   <Button type="primary" loading={busy} disabled={!sql.trim()} onClick={()=>void execute()}>{t('执行结构 SQL')}</Button>
  </Space>
 </Card>;
}
