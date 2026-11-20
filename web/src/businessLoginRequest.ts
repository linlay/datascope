// Business authentication is public and must not reuse platform sessions.
export async function publicRequest<T>(path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
 const response = await fetch(path, {
  method: body === undefined ? 'GET' : 'POST', credentials: 'omit', cache: 'no-store', signal,
  headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body),
 });
 const data = await response.json().catch(() => null);
 if (!response.ok) throw new Error(typeof data?.error === 'string' ? data.error : (response.status === 429 ? '登录过于频繁，请稍后再试' : '暂时无法登录，请稍后重试'));
 if (!data || typeof data !== 'object' || Array.isArray(data)) throw new Error('业务登录接口返回无效数据，请检查部署路径和反向代理配置');
 return data as T;
}
