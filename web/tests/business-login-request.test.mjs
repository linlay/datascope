import { test } from 'node:test';
import assert from 'node:assert/strict';
import { publicRequest } from '../src/businessLoginRequest.ts';

test('public login requests preserve the deployment path and omit platform credentials', async t => {
 const calls = [];
 t.mock.method(globalThis, 'fetch', async (path, options) => {
  calls.push({ path, ...options });
  return Response.json({ database_id: 'db1' });
 });
 const path = '/datascope/auth/databases/db1';
 await publicRequest(`${path}/login-info`);
 await publicRequest(`${path}/token`, { username: '001', password: 'test-password' });
 assert.deepEqual(calls.map(call => call.path), [`${path}/login-info`, `${path}/token`]);
 assert.deepEqual(calls.map(call => call.method), ['GET', 'POST']);
 for (const call of calls) {
  assert.equal(call.credentials, 'omit');
  assert.equal(call.cache, 'no-store');
 }
 assert.equal(calls[0].body, undefined);
 assert.deepEqual(JSON.parse(calls[1].body), { username: '001', password: 'test-password' });
});

test('HTML fallback pages and empty JSON responses produce a visible error', async t => {
 for (const body of ['<!DOCTYPE html><title>Portal</title>', 'null', '[]', '"unexpected"']) {
  const mock = t.mock.method(globalThis, 'fetch', async () => new Response(body));
  await assert.rejects(publicRequest('/datascope/auth/databases/db1/login-info'), /部署路径和反向代理/);
  mock.mock.restore();
 }
});

test('credential errors and rate limits retain their login error messages', async t => {
 const mock = t.mock.method(globalThis, 'fetch', async () => Response.json({ error: '账号或密码错误' }, { status: 401 }));
 await assert.rejects(publicRequest('/token', {}), /账号或密码错误/);
 mock.mock.restore();
 t.mock.method(globalThis, 'fetch', async () => new Response('Too many requests', { status: 429 }));
 await assert.rejects(publicRequest('/token', {}), /登录过于频繁/);
});
