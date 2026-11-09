import assert from 'node:assert/strict';
import { test } from 'node:test';
import { strFromU8, unzipSync } from 'fflate';
import { buildConnectorZip } from '../src/connectorZip.ts';

const endpoint = new URL('https://mcp.example.com/mcp/custom-route');
const cases = [
  ['NONE', 'none', undefined],
  ['OAUTH', 'mcp', undefined],
  ['ONEID', 'oneid', { Authorization: 'Bearer ${ONEID_TOKEN}' }],
  ['TOKEN', 'token', { Authorization: 'Bearer ${API_KEY}' }],
  ['TOKEN', 'token', { 'X-API-Key': '${API_KEY}' }, 'X-API-Key'],
  ['ACCOUNT', 'token', { Authorization: 'Bearer ${BUSINESS_TOKEN}' }],
];
for (const [mode, authMode, headers, header] of cases) {
  test(`export ${mode} with ${header || 'default headers'}`, () => {
    const db = { id: 'A'.repeat(64), name: '业务库', revision: 7, mcp_enabled: true,
      access: { policy: { mode }, revision: 3, provider: { header_name: header, secret_env: 'PRIVATE_ENV_NAME' } } };
    const { bytes } = buildConnectorZip(db, endpoint);
    const files = unzipSync(bytes);
    const read = name => JSON.parse(strFromU8(files[name]));
    const manifest = read('connector.json');
    assert.equal(manifest.authMode, authMode);
    assert.match(manifest.id, /^[a-z0-9][a-z0-9._-]{0,63}$/);
    assert.equal(manifest.name, db.name);
    assert.equal(manifest.version, '1.7.3');
    const server = read('mcp.json').mcpServers.main;
    assert.equal(server.url, endpoint.href);
    assert.equal(server.type, 'streamableHttp');
    assert.deepEqual(server.headers, headers);
    assert.equal(!!files['token-schema.json'], authMode === 'token');
    if (authMode === 'token') {
      const [field] = read('token-schema.json').fields;
      assert.equal(field.type, 'password');
      assert.equal(field.required, true);
      assert.equal(field.defaultValue, undefined);
      assert.ok(Object.values(headers)[0].includes('${' + field.key + '}'));
    }
    for (const bytes of Object.values(files)) assert.ok(!strFromU8(bytes).includes('PRIVATE_ENV_NAME'));
  });
}

test('export requires an enabled service, resolved policy and distributable endpoint', () => {
  const db = { mcp_enabled: true, access: { policy: { mode: 'NONE' } } };
  assert.throws(() => buildConnectorZip({ mcp_enabled: false }, endpoint), /开启/);
  assert.throws(() => buildConnectorZip({ mcp_enabled: true }, endpoint), /认证配置/);
  assert.throws(() => buildConnectorZip(db, new URL('http://remote.test/mcp')), /HTTPS/);
  assert.throws(() => buildConnectorZip(db, new URL('https://remote.test/mcp?token=secret')), /查询参数/);
});
