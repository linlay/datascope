package action

// Ordinary value helpers follow qiuer's formal runtime. I/O and transaction
// authority are deliberately supplied by the Action host, not by this prelude.
const nativePrelude = `
class HttpError extends Error {
  constructor(status, message, details) {
    super(message); this.name = 'HttpError'; this.status = status;
    if (details !== undefined) this.details = details;
  }
}
globalThis.HttpError = HttpError;
(function(global) {
  const own = (value,key) => value != null && Object.prototype.hasOwnProperty.call(value,key);
  global.nil = null;
  global.list = {
    size: value => value == null ? 0 : Array.isArray(value) || typeof value === 'string' ? value.length : Object.keys(value).length,
    contains: (value,item) => Array.isArray(value) && value.includes(item),
    indexOf: (value,item) => Array.isArray(value) ? value.indexOf(item) : -1
  };
  global.map = {contains: (value,key) => own(value,key) && value[key] != null, get: (value,key) => value == null ? null : value[key]};
  global.string = {
    replace: (value,from,to) => value == null ? value : String(value).split(from).join(String(to)),
    upper: value => value == null ? value : String(value).toUpperCase(),
    lower: value => value == null ? value : String(value).toLowerCase(),
    indexOf: (value,part) => value == null ? -1 : String(value).indexOf(part),
    contains: (value,part) => value != null && String(value).includes(part)
  };
  global.coalesce = (...values) => values.find(value => value != null) ?? null;
  global.typeOf = value => value == null ? 'nil' : Array.isArray(value) ? 'list' : typeof value === 'object' ? 'map' : typeof value;
  global.contains = (path,source = global.$context) => {
    let value = source;
    for (const key of String(path).split('.')) { if (!own(value,key)) return false; value = value[key]; }
    return value != null;
  };
})(globalThis);
`
