import type { DatabaseConnection } from '../api';

export interface ParsedConnection { driver: string; path?: string; password?: string; connection?: DatabaseConnection }
export const defaultPorts: Record<string, number> = { mysql: 3306, postgresql: 5432, oracle: 1521, dameng: 5236 };

// Parse locally: the original URL, including credentials, never leaves the form.
export function parseConnectionURL(raw: string): ParsedConnection {
 let text = raw.trim().replace(/^jdbc:/i, '');
 if (/^sqlite:/i.test(text)) {
  let path = text.replace(/^sqlite:/i, '');
  if (path.startsWith('///')) path = path.slice(2);
  if (!path || path === ':memory:' || /[?#]/.test(path)) throw new Error('请填写持久化 SQLite 文件路径');
  return { driver: 'sqlite', path: decodeURIComponent(path) };
 }
 // JDBC Oracle thin uses @//host:port/service; credentials may be supplied in the URL.
 if (/^oracle:thin:/i.test(text) && /@(?:\/\/)?[^/]+:\d+:[^/?]+(?:\?|$)/.test(text)) throw new Error('Oracle 请使用 service name URL（@//host:port/service），暂不支持 SID');
 text = text.replace(/^oracle:thin:(?:([^/@]+)\/([^@]*)@|@)(?:\/\/)?/i, (_, user, pass) => `oracle://${user ? `${encodeURIComponent(user)}:${encodeURIComponent(pass)}@` : ''}`);
 let url: URL;
 try { url = new URL(text); } catch { throw new Error('连接 URL 格式无效，请使用 数据库类型://账号:密码@地址:端口/数据库'); }
 const scheme = url.protocol.slice(0, -1).toLowerCase();
 const driver = ({ postgres: 'postgresql', postgresql: 'postgresql', mysql: 'mysql', oracle: 'oracle', dm: 'dameng', dameng: 'dameng' } as Record<string, string>)[scheme];
 if (!driver || !url.hostname || url.hash) throw new Error('不支持该数据库 URL，请检查类型和地址');
 const q = url.searchParams;
 const allowed = new Set(['user','username','password','schema','currentSchema','sslmode','sslMode','tls_mode','useSSL','requireSSL','verifyServerCertificate','service_name','serverTimezone','characterEncoding','useUnicode']);
 for (const key of q.keys()) if (!allowed.has(key)) throw new Error(`URL 参数 ${key} 暂不支持，请使用表单配置`);
 const database = decodeURIComponent(url.pathname.replace(/^\//, ''));
 const username = decodeURIComponent(url.username) || q.get('user') || q.get('username') || '';
 const password = decodeURIComponent(url.password) || q.get('password') || '';
 const port = url.port ? Number(url.port) : defaultPorts[driver];
 if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error('端口必须在 1–65535 之间');
 let tls_mode = q.get('tls_mode') || q.get('sslmode') || q.get('sslMode') || ((driver === 'postgresql' || driver === 'mysql') ? 'verify-full' : undefined);
 if (q.get('useSSL') === 'false') tls_mode = 'disable';
 if (tls_mode === 'VERIFY_IDENTITY') tls_mode = 'verify-full';
 if (tls_mode === 'DISABLED') tls_mode = 'disable';
 if (tls_mode && !(driver === 'postgresql' ? ['verify-full','require','disable'] : driver === 'mysql' ? ['verify-full','disable'] : ['disable']).includes(tls_mode)) throw new Error('URL 中的 TLS 模式暂不支持，请在表单中选择');
 if (q.get('verifyServerCertificate') === 'false' || q.get('requireSSL') === 'false') throw new Error('请在表单中明确选择连接加密方式');
 return { driver, password, connection: { host: url.hostname.replace(/^\[|\]$/g, ''), port, username, database: driver === 'mysql' || driver === 'postgresql' ? database : undefined, service_name: driver === 'oracle' ? q.get('service_name') || database : undefined, schema: driver === 'mysql' ? database : q.get('schema') || q.get('currentSchema') || (driver === 'postgresql' ? 'public' : ''), tls_mode } };
}
