import { DatabaseOutlined } from '@ant-design/icons';
import sqlite from '../assets/databases/sqlite.svg';
import oceanbase from '../assets/databases/oceanbase.svg';
import mysql from '../assets/databases/mysql.svg';
import postgresql from '../assets/databases/postgresql.svg';
import oracle from '../assets/databases/oracle.svg';
import dameng from '../assets/databases/dameng.svg';

const logos: Record<string, string> = { sqlite, mysql, oceanbase, postgresql, oracle, dameng };

export function DatabaseIcon({ driver }: { driver: string }) {
 const src = logos[driver];
 return <span aria-hidden="true" style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 20, height: 20, flexShrink: 0, verticalAlign: 'middle' }}>
  {src ? <img src={src} alt="" draggable={false} width={18} height={18} style={{ display: 'block', objectFit: 'contain' }} /> : <DatabaseOutlined style={{ fontSize: 18 }} />}
 </span>;
}
