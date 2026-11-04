import { Alert, Space, Tag, Typography } from 'antd';
import { managementCall } from '../api';
import { useI18n } from '../ui/preferences';

export interface Invocation<T> {
 arguments: Record<string, unknown>;
 elapsedMS: number;
 data?: T;
 error?: string;
}

// These calls simulate business tool workflows using platform authorization.
export async function invoke<T>(databaseID: string, operation: string, args: Record<string, unknown>): Promise<Invocation<T>> {
 const start = performance.now();
 try {
  const data = await managementCall<T>(operation, { database_id: databaseID, ...args });
  return { arguments: args, data, elapsedMS: Math.round(performance.now() - start) };
 } catch (error) {
  return { arguments: args, error: (error as Error).message, elapsedMS: Math.round(performance.now() - start) };
 }
}

export function JsonData({ value }: { value: unknown }) {
 const t = useI18n();
 const text = JSON.stringify(value, null, 2);
 return <div className="tool-call-data">
  <Typography.Text className="tool-call-copy" copyable={{ text }}>{t('复制 JSON')}</Typography.Text>
  <pre className="tool-call-json">{text}</pre>
 </div>;
}

export function CallDetails({ invocation, failed = false }: { invocation: Invocation<unknown>; failed?: boolean }) {
 const t = useI18n();
 const rejected = failed || !!invocation.error;
 return <div className="tool-call-details">
  <Space wrap>
   <Typography.Text type="secondary">{t('调用参数')}</Typography.Text>
   <Tag color={rejected ? 'error' : 'success'}>{t(rejected ? '失败' : '成功')}</Tag>
   <Typography.Text type="secondary">{invocation.elapsedMS} ms</Typography.Text>
  </Space>
  <JsonData value={invocation.arguments} />
  {invocation.error && <Alert type="error" showIcon title={invocation.error} />}
 </div>;
}
