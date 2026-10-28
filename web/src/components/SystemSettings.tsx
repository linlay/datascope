import { Button, Card, Drawer, Select, Space, Typography } from 'antd';
import { usePreferences, type Appearance, type Language } from '../ui/preferences';

export function SystemSettings({open,onClose}:{open:boolean;onClose:()=>void}){
 const {language,setLanguage,appearance,setAppearance,t}=usePreferences();
 return <Drawer rootClassName="system-settings-drawer" title={t('系统设置')} open={open} onClose={onClose} size={440}>
  <Space orientation="vertical" size="large" style={{width:'100%'}}>
   <Space orientation="vertical" style={{width:'100%'}}>
    <Typography.Text strong>{t('界面语言')}</Typography.Text>
    <Select<Language> aria-label={t('界面语言')} value={language} onChange={setLanguage} style={{width:'100%'}} options={[{value:'zh-CN',label:'简体中文'},{value:'en',label:'English'}]}/>
   </Space>
   <Space orientation="vertical" style={{width:'100%'}}>
    <Typography.Text strong>{t('外观')}</Typography.Text>
    <Select<Appearance> aria-label={t('外观')} value={appearance} onChange={setAppearance} style={{width:'100%'}} options={[{value:'system',label:t('跟随系统')},{value:'light',label:t('浅色')},{value:'dark',label:t('深色')}]}/>
    <Typography.Text type="secondary">{t('设置应用于此浏览器的整个网站。')}</Typography.Text>
   </Space>
   <Card size="small" title={t('管理连接器')}>
    <Typography.Paragraph>{t('使用平台账号授权，权限与管理后台一致。')}</Typography.Paragraph>
    <Button type="primary" href="/api/connectors/datascope-admin.zip">{t('下载连接器 ZIP')}</Button>
   </Card>
  </Space>
 </Drawer>;
}
