// Preserve Go's RFC3339Nano precision before subtracting epoch timestamps.
function timestampNanos(value:string):bigint|undefined {
 const match=/^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
 if(!match)return undefined;
 const milliseconds=Date.parse(match[1]+match[3]);
 if(!Number.isFinite(milliseconds))return undefined;
 return BigInt(milliseconds)*1000000n+BigInt((match[2]??'').padEnd(9,'0'));
}

export function auditDuration(event:{started_at:string;finished_at?:string}):string {
 if(!event.finished_at)return '—';
 const start=timestampNanos(event.started_at),end=timestampNanos(event.finished_at);
 if(start===undefined||end===undefined||end<start)return '—';
 const elapsed=end-start;
 if(elapsed>0n&&elapsed<1000n)return '<0.001 ms';
 return `${Number((Number(elapsed)/1000000).toFixed(3))} ms`;
}
