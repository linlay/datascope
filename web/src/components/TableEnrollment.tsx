import { useEffect,useState } from 'react';
import { Alert,Button,Input,Modal,Space,Table } from 'antd';
import { databases,type CandidateTable,type Database } from '../api';
import { useI18n } from '../ui/preferences';

export function TableEnrollment({db,disabled,onSaved}:{db:Database;disabled:boolean;onSaved:(db:Database)=>void}) {
 const t=useI18n();const [open,setOpen]=useState(false);const [revision,setRevision]=useState(db.revision);const [query,setQuery]=useState('');const [page,setPage]=useState(1);
 const [rows,setRows]=useState<CandidateTable[]>([]);const [total,setTotal]=useState(0);const [selected,setSelected]=useState<string[]>([]);
 const [loading,setLoading]=useState(false);const [saving,setSaving]=useState(false);const [error,setError]=useState<string>();
 useEffect(()=>{if(!open)return;let live=true;setLoading(true);setError(undefined);
  databases.candidates(db.id,query,20,(page-1)*20).then(result=>{if(live){setRows(result.items);setTotal(result.total)}}).catch(e=>{if(live)setError(e.message)}).finally(()=>{if(live)setLoading(false)});return()=>{live=false};
 },[open,db.id,query,page]);
 return <>
  <Button disabled={disabled} onClick={()=>{setRevision(db.revision);setSelected((db.discovery?.tables??[]).map(x=>x.name));setPage(1);setQuery('');setOpen(true)}}>{t('选择登记表')}</Button>
  <Modal open={open} title={t('选择登记表')} width={800} confirmLoading={saving} okButtonProps={{disabled:loading}} okText={t('登记所选表')} onCancel={()=>{if(!saving)setOpen(false)}} onOk={async()=>{
   setSaving(true);setError(undefined);try{onSaved(await databases.enroll(db.id,revision,selected));setOpen(false)}catch(e){setError((e as Error).message)}finally{setSaving(false)};
  }}>
   <Space orientation="vertical" style={{width:'100%'}}>
    <Alert type="info" showIcon title={t('这里只读取候选表名，登记后才读取字段和注释。新登记表默认关闭，请随后配置开放权限。')}/>
    {error&&<Alert type="error" title={error}/>}
    <Input.Search aria-label={t('搜索候选表')} placeholder="Schema.TABLE_NAME" value={query} onChange={e=>{setQuery(e.target.value);setPage(1)}} allowClear/>
    <Table<CandidateTable> rowKey="name" loading={loading} dataSource={rows} rowSelection={{selectedRowKeys:selected,preserveSelectedRowKeys:true,onChange:keys=>setSelected(keys.map(String))}} pagination={{current:page,pageSize:20,total,onChange:setPage,showSizeChanger:false}} columns={[{title:'Schema',dataIndex:'schema'},{title:t('完整表名'),dataIndex:'name'}]}/>
    <span>{t('已选择 {0} 张表',{'0':selected.length})}</span>
   </Space>
  </Modal>
 </>;
}
