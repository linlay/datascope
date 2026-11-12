import { useEffect, useState } from 'react';
import { Button, Form, Input, Radio, Space, Upload, message } from 'antd';
import { useI18n } from '../ui/preferences';

export function OneIDKeyInput({ jwksUrl, publicKeyPem, onChange, disabled }: {
 jwksUrl?: string; publicKeyPem?: string; disabled?: boolean;
 onChange: (patch: { jwks_url: string; public_key_pem: string }) => void;
}) {
 const t = useI18n();
 const [source, setSource] = useState(publicKeyPem ? 'PEM' : 'JWKS');
 useEffect(() => { if (publicKeyPem) setSource('PEM'); else if (jwksUrl) setSource('JWKS'); }, [publicKeyPem, jwksUrl]);
 return <>
  <Form.Item label={t('验签公钥来源')}>
   <Radio.Group disabled={disabled} value={source} onChange={e => { setSource(e.target.value); onChange({ jwks_url: '', public_key_pem: '' }); }} options={[{ value: 'JWKS', label: t('JWKS 地址') }, { value: 'PEM', label: 'publickey.pem' }]} />
  </Form.Item>
  {source === 'JWKS' ? <Form.Item label={t('签名公钥地址（JWKS）')} required>
   <Input disabled={disabled} aria-label={t('签名公钥地址（JWKS）')} value={jwksUrl || ''} onChange={e => onChange({ jwks_url: e.target.value, public_key_pem: '' })} placeholder="https://identity.example.com/oidc/jwks" />
  </Form.Item> : <Form.Item label={t('RSA 公钥（PEM）')} required>
   <Space orientation="vertical" style={{ width: '100%' }}>
    <Upload disabled={disabled} accept=".pem,.pub" showUploadList={false} beforeUpload={async file => {
     if (file.size > 64 * 1024) { message.error(t('公钥文件不能超过 64 KiB')); return false; }
     try { onChange({ jwks_url: '', public_key_pem: await file.text() }); } catch { message.error(t('读取公钥文件失败')); }
     return false;
    }}><Button disabled={disabled}>{t('导入 publickey.pem')}</Button></Upload>
    <Input.TextArea disabled={disabled} aria-label={t('RSA 公钥（PEM）')} rows={7} value={publicKeyPem || ''} onChange={e => onChange({ jwks_url: '', public_key_pem: e.target.value })} placeholder={'-----BEGIN PUBLIC KEY-----\n…\n-----END PUBLIC KEY-----'} />
   </Space>
  </Form.Item>}
 </>;
}
