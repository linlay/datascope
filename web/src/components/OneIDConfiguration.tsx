import { useEffect, useRef, useState } from 'react';
import { Alert, Button, Form, Input, InputNumber, Radio, Space, Upload, message } from 'antd';
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

// Emit invalid partial rows as invalid config, so Save cannot silently drop a binding.
export function IdentityMappingsInput({ value, onChange, disabled }: {
 value?: Record<string, number>; onChange?: (value: Record<string, number> | undefined) => void; disabled?: boolean;
}) {
 const t = useI18n();
 const [rows, setRows] = useState(() => Object.entries(value || {}));
 const last = useRef(JSON.stringify(value));
 useEffect(() => { const incoming = JSON.stringify(value); if (incoming !== last.current) { setRows(Object.entries(value || {})); last.current = incoming; } }, [value]);
 const duplicate = new Set(rows.map(([external]) => external)).size !== rows.length;
 const emit = (next: [string, number][]) => {
  setRows(next);
  const duplicated = new Set(next.map(([external]) => external)).size !== next.length;
  const updated = next.length ? duplicated ? { '': 0 } : Object.fromEntries(next) : undefined;
  last.current = JSON.stringify(updated); onChange?.(updated);
 };
 return <Space orientation="vertical" style={{ width: '100%' }}>
  <Alert type="info" showIcon title={t('配置映射后，仅已登记的企业身份可访问本库；不配置时直接使用业务身份字段值。')} />
  {rows.map(([external, id], index) => <Space key={index} wrap>
   <Input disabled={disabled} aria-label={t('企业身份值') + ` ${index + 1}`} placeholder={t('企业工号，如 E1001')} value={external} onChange={e => emit(rows.map((row, i) => i === index ? [e.target.value, id] : row))} />
   <span>→</span><InputNumber disabled={disabled} aria-label={t('本库员工编号') + ` ${index + 1}`} min={1} precision={0} placeholder={t('本库员工编号')} value={id || undefined} onChange={v => emit(rows.map((row, i) => i === index ? [external, v || 0] : row))} />
   <Button disabled={disabled} danger onClick={() => emit(rows.filter((_, i) => i !== index))}>{t('移除')}</Button>
  </Space>)}
  {duplicate && <Alert type="error" title={t('企业身份值不能重复')} />}
  <Button disabled={disabled} onClick={() => emit([...rows, ['', 0]])}>{t('增加身份映射')}</Button>
 </Space>;
}
