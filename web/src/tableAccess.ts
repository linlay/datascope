import type { PolicyView, TableSchema } from './api';

// Batch table access changes use the complete registered schema, independently
// of the directory's search filter. Existing column scopes and row rules stay
// intact. A table with no selected columns needs an explicit current-column
// list before it can be enabled; this does not include future columns.
export function setAllTableAccess(policies: PolicyView[], tables: TableSchema[], enabled: boolean): PolicyView[] {
 const schemas=new Map(tables.map(table=>[table.name.toLowerCase(),table]));
 return policies.map(policy=>{
  const schema=schemas.get(policy.table.toLowerCase());
  if(!schema)return policy;
  const columns=enabled&&policy.columns!=null&&policy.columns.length===0?schema.columns.map(column=>column.name):policy.columns;
  return {...policy,columns,enabled:enabled&&(columns==null||columns.length>0)};
 });
}
