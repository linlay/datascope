import { Switch, Tooltip } from 'antd';
import { useI18n } from '../ui/preferences';

export function PublicationSwitch({ name, published, disabled, loading, reason, onChange }: {
 name: string; published: boolean; disabled?: boolean; loading?: boolean; reason?: string; onChange: (published: boolean) => void;
}) {
 const t = useI18n();
 return <Tooltip title={reason || t(published ? '已发布' : '草稿')}>
  <span className="resource-publication"><Switch size="small" aria-label={t('发布 {0}', { '0': name })} checked={published} disabled={disabled} loading={loading} onChange={onChange} /></span>
 </Tooltip>;
}
