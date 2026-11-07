import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseConnectionURL } from '../src/components/connectionURL.ts';

test('PostgreSQL URL decodes credentials and honors schema, IPv6 and TLS', () => {
 const result = parseConnectionURL('postgres://reader:p%40ss%3Aword@[::1]:5544/business?sslmode=require&schema=sales');
 assert.equal(result.driver, 'postgresql');
 assert.equal(result.password, 'p@ss:word');
 assert.deepEqual(result.connection, {host:'::1',port:5544,username:'reader',database:'business',schema:'sales',tls_mode:'require',service_name:undefined});
});
test('JDBC MySQL and PostgreSQL URLs populate editable fields', () => {
 const mysql = parseConnectionURL('jdbc:mysql://localhost/shop?user=reader&password=secret&useSSL=false');
 assert.equal(mysql.connection.port, 3306);
 assert.equal(mysql.connection.schema, 'shop');
 assert.equal(mysql.connection.tls_mode, 'disable');
 assert.equal(mysql.password, 'secret');
 assert.equal(parseConnectionURL('jdbc:postgresql://localhost/app').connection.schema,'public');
});
test('Oracle service and Dameng URLs', () => {
 const oracle = parseConnectionURL('jdbc:oracle:thin:@//localhost:1521/FREEPDB1?user=business&password=secret');
 assert.equal(oracle.connection.service_name,'FREEPDB1');
 assert.equal(oracle.connection.schema,'');
 assert.equal(parseConnectionURL('oracle://reader:pass@localhost/FREEPDB1?schema=APP').connection.schema,'APP');
 assert.equal(parseConnectionURL('jdbc:oracle:thin:business/secret@localhost:1521/FREEPDB1').connection.service_name,'FREEPDB1');
 assert.throws(()=>parseConnectionURL('jdbc:oracle:thin:@localhost:1521:ORCL'));
 assert.equal(parseConnectionURL('dm://user:pass@localhost?schema=APP').connection.port,5236);
});
test('SQLite URLs preserve relative and absolute server paths', () => {
 assert.equal(parseConnectionURL('jdbc:sqlite:data/local.db').path,'data/local.db');
 assert.equal(parseConnectionURL('sqlite:///srv/local.db').path,'/srv/local.db');
});
test('unsupported, lossy and malformed URLs are rejected', () => {
 for (const value of ['https://localhost/db','postgres://localhost:99999/db','sqlite::memory:','mysql://localhost/db?socket=/tmp/mysql.sock','postgres://localhost/db?sslmode=prefer','postgres://u:%ZZ@localhost/db']) {
  assert.throws(()=>parseConnectionURL(value),value);
 }
});
