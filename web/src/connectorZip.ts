import { strToU8, zipSync } from 'fflate';
import type { Database } from './api';

const modes = { NONE: 'none', OAUTH: 'mcp', ONEID: 'oneid', TOKEN: 'token', ACCOUNT: 'token' } as const;

// Preserve case-sensitive database identities across installations while fitting gen-agent's 64-character ID limit.
function connectorId(origin: string, databaseId: string) {
  let hash = 2166136261;
  for (const byte of strToU8(origin + '/' + databaseId)) hash = Math.imul(hash ^ byte, 16777619);
  return `datascope-${databaseId.toLowerCase().slice(0, 45)}-${(hash >>> 0).toString(16).padStart(8, '0')}`;
}

export function buildConnectorZip(db: Database, endpoint: URL) {
  if (!db.mcp_enabled) throw new Error('请先开启并保存 MCP 服务');
  const access = db.access;
  if (!access || !Object.prototype.hasOwnProperty.call(modes, access.policy.mode)) throw new Error('无法读取已生效的业务认证配置');
  const { mode } = access.policy;
  const local = ['localhost', '127.0.0.1', '[::1]'].includes(endpoint.hostname);
  if (endpoint.protocol !== 'https:' && !(local && endpoint.protocol === 'http:')) {
    throw new Error('连接器需要 HTTPS 地址，本机测试可使用回环 HTTP 地址');
  }
  if (endpoint.username || endpoint.password || endpoint.search || endpoint.hash) throw new Error('MCP 地址不能包含凭据、查询参数或片段');
  if (mode === 'ONEID' && (endpoint.protocol !== 'https:' || endpoint.port)) throw new Error('企业 SSO 需要使用默认端口的 HTTPS 地址');

  const id = connectorId(endpoint.origin, db.id);
  const version = `1.${db.revision}.${access.revision}`;
  const files: Record<string, Uint8Array> = {};
  const addJSON = (name: string, value: unknown) => { files[name] = strToU8(JSON.stringify(value, null, 2) + '\n'); };
  addJSON('connector.json', { id, name: db.name, type: 'mcp', authMode: modes[mode], version });

  let headers: Record<string, string> | undefined;
  if (mode === 'ONEID') headers = { Authorization: 'Bearer ${ONEID_TOKEN}' };
  if (mode === 'TOKEN' || mode === 'ACCOUNT') {
    const personal = mode === 'ACCOUNT';
    const key = personal ? 'BUSINESS_TOKEN' : 'API_KEY';
    const label = personal ? '个人业务 Token' : 'API Key';
    const header = !personal && access.provider.header_name;
    const placeholder = '${' + key + '}';
    headers = header ? { [header]: placeholder } : { Authorization: 'Bearer ' + placeholder };
    addJSON('token-schema.json', { title: db.name + ' · ' + label, fields: [{ key, label, required: true, type: 'password' }] });
  }
  addJSON('mcp.json', { mcpServers: { main: { type: 'streamableHttp', url: endpoint.href, headers } } });
  return { filename: `${id}-${version}.zip`, bytes: zipSync(files, { level: 0, mtime: new Date(2000, 0, 1) }) };
}
