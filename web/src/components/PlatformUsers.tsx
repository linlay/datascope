import { useI18n } from '../ui/preferences';
import { useEffect, useState } from 'react';
import { Alert, Button, Form, Input, Modal, Space, Table, Tag, message } from 'antd';
import { managementCall } from '../api';
import type { PlatformUser } from '../Auth';

// Platform-level user administration, opened from the system settings drawer.
export function PlatformUsersDialog({open,onClose}:{open:boolean;onClose:()=>void}){
 const t=useI18n();
 const [users,setUsers]=useState<PlatformUser[]>([]);
 const [error,setError]=useState<string>();
 const [adding,setAdding]=useState(false);
 const [busy,setBusy]=useState(false);
 const [form]=Form.useForm();
 const refresh=async()=>{try{setUsers(await managementCall<PlatformUser[]>('list_users'));setError(undefined);}catch(e){setError((e as Error).message);}};
 useEffect(()=>{if(open)void refresh();},[open]);
 const run=async(action:string,args:Record<string,unknown>)=>{setBusy(true);try{await managementCall(action,args);setAdding(false);form.resetFields();await refresh();message.success(t("已保存"));}catch(e){setError((e as Error).message);}finally{setBusy(false);}};
 return <>
 <Modal title={t("平台用户管理")} open={open} onCancel={onClose} width={720} footer={<Button onClick={onClose}>{t("关闭")}</Button>}>
  <Space orientation="vertical" style={{width:'100%'}} size="middle">
   {error&&<Alert type="error" title={error} closable onClose={()=>setError(undefined)}/>}
   <div style={{display:'flex',justifyContent:'flex-end'}}><Button type="primary" onClick={()=>setAdding(true)}>{t("添加平台用户")}</Button></div>
   <Table rowKey="id" dataSource={users} columns={[
    {title:'ID',dataIndex:'id'},
    {title:t("用户名"),dataIndex:'username'},
    {title:t("姓名"),dataIndex:'name'},
    {title:t("状态"),render:(_,u:PlatformUser)=><Tag color={u.active?'green':'default'}>{u.active?t("正常"):t("已禁用")}</Tag>},
    {title:t("操作"),render:(_,u:PlatformUser)=><Space>
     <Button disabled={u.system_admin||busy} onClick={()=>Modal.confirm({title:`${u.active?t("禁用"):t("启用")} ${u.username}？`,content:t("登录会话将撤销，现有 MCP 令牌也会被拒绝。"),onOk:()=>run(u.active?'disable_user':'enable_user',{user_id:u.id})})}>{u.active?t("禁用"):t("启用")}</Button>
     <Button disabled={busy} onClick={()=>{let password='';Modal.confirm({title:t("重置 {0} 的密码",{"0":u.username}),content:<Input.Password autoComplete="new-password" onChange={e=>password=e.target.value} placeholder={t("至少 6 字符")}/>,onOk:()=>run('reset_password',{user_id:u.id,password})});}}>{t("重置密码")}</Button>
    </Space>}
   ]}/>
  </Space>
 </Modal>
 <Modal title={t("创建平台用户")} open={adding} onCancel={()=>setAdding(false)} confirmLoading={busy} onOk={()=>void form.validateFields().then(v=>run('create_user',v))}>
  <Form form={form} layout="vertical">
   <Form.Item name="username" label={t("用户名")} rules={[{required:true}]}><Input autoComplete="off"/></Form.Item>
   <Form.Item name="name" label={t("姓名")} rules={[{required:true}]}><Input/></Form.Item>
   <Form.Item name="password" label={t("初始密码")} rules={[{required:true,min:6}]}><Input.Password autoComplete="new-password"/></Form.Item>
  </Form>
 </Modal>
 </>;
}
