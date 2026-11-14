import { managementCall, type DatabaseConnection } from './api';

export interface SourceConnection {
 id: string; name: string; driver: string; path?: string; connection?: DatabaseConnection; revision: number; has_password: boolean;
}
export interface ImportSchedule { mode: 'manual' | 'once' | 'interval' | 'daily'; at?: string; minutes?: number; time?: string; timezone: string }
export interface ImportTask {
 id: string; name: string; description: string; script: string; sources: string[]; parameters: Record<string, unknown>;
 schedule: ImportSchedule; enabled: boolean; timeout: number; revision: number; owner: number;
 next_run?: string; checkpoint?: Record<string, unknown>; last_run?: ImportRun;
}
export interface ImportRun {

 script?: string;
 id: string; task_id: string; task_name: string; revision: number; actor: number; trigger: string; status: string;
 started: string; finished?: string; read: number; written: number; logs: string[]; error?: string;
 parameters?: Record<string, unknown>; checkpoint?: Record<string, unknown>;
}
export interface SourceOverview { connections: SourceConnection[]; tasks: ImportTask[]; runs: ImportRun[] }
export const sourceCall = <T,>(database: string, action: string, args: Record<string, unknown> = {}) =>
 managementCall<T>(action, { ...args, database_id: database });

// A blank task never assumes a business table or runs automatically.
export const newImportTask = (): ImportTask => ({
 id: '', name: '', description: '', sources: [], parameters: {}, enabled: false, timeout: 300, revision: 0, owner: 0,
 schedule: { mode: 'manual', timezone: 'Asia/Shanghai', minutes: 30, time: '02:00' },
 script: `function main(params, context) {
  // sources.get("connection_alias").query(sql, [values])
  // db.target().query(sql, [values])
  // db.target().update(sql, [values])
  // context.checkpoint / context.saveCheckpoint({ ... })
  context.log("Import started");
}
`,
});
