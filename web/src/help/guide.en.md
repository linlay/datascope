# DataScope user guide

## Getting started

Choose a database in the top bar. Your platform permissions for that database determine which pages appear. Open the SQL workbench to query data or the action workbench to run a configured operation.

When connecting a database for the first time, add and test the connection, expose one table in query configuration, and run a query as a business user. Verify the visible rows and fields before enabling tools for a client.

## Databases, accounts and settings

Administrators can register a network database, connect or create a SQLite database, or upload a SQLite or CSV file up to 20 MiB. Use UTF-8 for CSV. The first row supplies column names and values are imported as text into a table named `data`.

For a network database, configure the schema scope before enrolling tables. When cloning configuration, check the source, destination and preview before applying changes. When editing the schema scope, existing action write connections inherit the default schema and allowed scope while retaining their separate write account, password and TLS settings.

System administrators can open Database configuration → Basic information → Edit connection to maintain a network database's host, port, read-only account, password and TLS. Leave the password blank to keep it; new passwords are encrypted. Use Test connection to check the draft, then Validate and save. Saving repeats the connection test and checks that enrolled columns still exist with compatible types, nullability and primary-key attributes. Failure preserves the previous configuration and password. Existing action write connections receive the same host, port and TLS settings, keep their separate account and password, and are also tested. Refresh and retry if another window has saved changes. Successful edits retain the source ID, permissions and business configuration and create an audit entry. This entry is for maintenance of the same business database; matching structure cannot prove that business data is identical. Create a new source to switch business databases or database types. SQLite has no editing entry, and database names and schema scope are not changed in this dialog.

Platform accounts sign in to the console. Database grants control access to configuration and workbenches. Business identities determine data visibility and authorization for real actions. Configure business authentication and capabilities for the selected database. Open system settings in the top bar to change language or appearance and access authorized platform settings.

## Query configuration, SQL workbench and table data

Select a table in query configuration, expose the required fields, set row filters and limits, and configure masking for sensitive fields. Save the policy. Test with one record the business user should see and one they should not see.

In the SQL workbench, choose an identity and run a read-only SELECT. For a database with a customers table, try `SELECT customer_no FROM customers WHERE customer_no = :number` with parameters `{"number":"C001"}`. Inspect the rewritten SQL and results. Users with full query access can compare the raw results.

Use the table data page to browse and filter stored records with full query access. To check a business user's visibility, use that identity in the SQL workbench.

## Action configuration and script reference

New actions default to `main(params, context)`. Obtain the current database connection with `db.session()`, read with `conn.query` / `conn.queryOne`, and write with `conn.update`, using literal SQL and `:name` parameters. Queries do not automatically apply normal query permissions: scripts must check record ownership using `context.userId` and verify `result.rowsAffected` after writes. Export shared functions with `exports.fn` and load them with `require("./library")`. The following reference describes the compatible `run(args, ctx)` template; do not mix the two script APIs.

Create an action with a name, description and typed parameters. Implement the synchronous entry point `function run(args, ctx)`. Declare id as an integer and value as a string for the example below. Configure a write connection and enable writes before testing the action.

| Interface | Behavior |
| --- | --- |
| `my.one` | Applies query policies and returns the first visible row or null. |
| `my.all` | Applies the same policies and row limits and returns an array. |
| `db.one` / `db.all` | Read trusted data outside query policies. Return the first row or null, or an array, respectively. |
| `db.exec` | Runs one INSERT, UPDATE or DELETE and returns the affected row count. |
| `deny(message)` | Rejects the action, rolls back the transaction and returns the reason. |
| `ctx.userId` / `ctx.now` | The business identity and the fixed UTC time for this operation. |

Use backticks for SQL. Values inside `${...}` are bound parameters. Write table and column names directly in the template. This example assumes records has id, owner_id and value columns. The update checks ownership when writing.

```js
function run(args, ctx) {
  const row = my.one`SELECT id FROM records WHERE id = ${args.id}`;
  if (!row) deny("Record missing or inaccessible");
  const count = db.exec`UPDATE records SET value = ${args.value}
    WHERE id = ${args.id} AND owner_id = ${ctx.userId}`;
  if (count !== 1) deny("Record changed or update denied");
  return { id: args.id, updated: count };
}
```

To share logic, create a library in the same database, export a function with `export function`, and use `import { fn } from "library"` in the action.

**AI assistance** requires `config.read` and `action.write` on the database. Set `DATASCOPE_AI_ENDPOINT`, `DATASCOPE_AI_MODEL` and the provider's `DATASCOPE_AI_KEY` on admin-api and restart it. Use a gateway base ending in `/v1` or the full `/v1/chat/completions` endpoint. For a provider HTTP 404, check the endpoint and model name. For `AI service returned an invalid response`, inspect the detailed reason: `finish_reason=length` means truncated output; increase the provider output token limit or shorten the request. JSON or field-type errors require checking model JSON-object support. Complete JSON code fences and streams explicitly finished with `stop` but missing `[DONE]` are supported; incomplete output is rejected. The server reads model output incrementally and displays the validated proposal when complete. If parameter or script validation fails, the assistant attempts one correction using the diagnostic when the context size permits. The correction must pass all validation checks. Action SQL uses the shared parser subset, so `ILIKE` is unsupported even for PostgreSQL. For case-insensitive pattern matching, use `LOWER(name) LIKE LOWER(${args.pattern})` with a bound pattern value; verify collation and Unicode case behavior on the target database. If a proposal fails parameter or script validation, the error includes the specific cause (such as a parameter type, module syntax, library import or SQL template error). Add that cause to your request and regenerate, or edit the draft manually; rejected proposals are not applied. The default idle wait for the first or next output is 120 seconds. Administrators can adjust it with `DATASCOPE_AI_TIMEOUT`. If the error still reports 30s, check for an explicit old deployment value, set `DATASCOPE_AI_TIMEOUT=120s`, and restart admin-api; recreate containers when their environment changes. The reverse proxy must allow for the complete generation and a possible correction attempt. The bundled Docker management proxy waits up to 600 seconds; configure any outer gateway accordingly. Open **AI assistance** at the top right of the action editor to start a conversation in the right drawer. Try “Update items.name by id only when owner_id equals the current business identity.” The assistant uses the current draft, dialect, enabled table columns and library functions. Continue with follow-up conditions or clarification answers; each request includes the last six successful turns. The drawer shows request status, duration, a model-provided thinking summary and the response. The summary is a public explanation of the approach, not private reasoning. The request record explicitly states that the assistant does not call model tools or execute action SQL. You can stop generation while waiting. Compare the proposal with the draft, select parameters and script independently, then click **Apply to draft** and confirm. Review and save. Editing the draft or changing context invalidates old proposals. Conversation history stays in page memory. Manual editing remains available when the model is offline.

## Testing and executing actions

1. Fetch the action list in the action workbench. Select an action and inspect its parameter schema and confirmation requirements.
2. Enter JSON parameters and a business identity for a dry run. Inspect the return value and planned writes. Dry runs always roll back. Use a new request when testing changed input.
3. For a real call, publish the action, enable its MCP tools and grant the business account action capability. Authenticate with your own business identity on the real-call tab. If confirmation is required, open the returned link, review the preview and confirm.
4. If a call is interrupted, query the result with the same request_id. For an unknown outcome, check the audit log and business records before retrying.

In the rollback tab, editors who also have `impersonate` can select **Generate test arguments**, review and adopt the test case, then edit the arguments and expected result. Choose a business identity and run the trial yourself. For the same id, compare the write plan, affected rows and denial reason for the manager and a different user. **Explain trial** sends only parameter types, a diagnostic category or the return type and affected row counts. For an identified script defect, select **Take repair to draft** to generate and review an edit. Changing the editing context discards stale proposals.

## Knowledge configuration and workbench

Create entries for business terms, metric definitions or query examples in knowledge configuration. Choose a searchable name, save the content and check publication status. In the knowledge workbench, list entries, search for a business term and inspect the matching document's preview and JSON. Check the database, publication status and account capabilities if an entry is missing.

## View configuration and workbench

Create an HTML view from a template, enter its identifier, name and HTML, and save the draft. In the view workbench, preview with sample data, then use an identity preview to verify visible records, fields and empty results. After publication, load it in the real business preview with your own business identity. Actions invoked there can perform real writes. To recommend the view for queries, select it in the database's query display settings. If only the identity preview is empty, run the underlying query with the same database and identity and check the field names.

## Connecting an MCP client

`query` returns at most 200 rows by default, with a global ceiling of 1000 and additional per-table row limits; a multi-table query uses the smallest applicable limit. If `max_rows: 1000` still returns 200 rows, check query configuration and any SQL `LIMIT`. The response's `effective_max_rows` reports the actual cap. `limit_source` is `default`, `request`, `hard_limit`, or `table_policy`; when table policies further reduce the cap, `limiting_tables` identifies those tables. Existing policies are never automatically relaxed.

For summaries, use `SUM`, `COUNT`, `MIN`, `MAX`, `AVG` and `GROUP BY` directly, for example `SELECT natural_day, SUM(coop_fund) AS total FROM cashmgmt GROUP BY natural_day ORDER BY natural_day` (replace the table and column names with those exposed by your database). Aggregates still obey row permissions and column restrictions. MySQL and OceanBase MySQL mode support these ordinary aggregate calls without client-side `IGNORE_SPACE` configuration.

When details are necessary, paginate using a stable unique ordering key. For example, send `SELECT id, amount FROM records WHERE id > :arg_after ORDER BY id` with `parameters: {"arg_after": 0}` and `max_rows: 1000`; after saving each page successfully, use its final id for the next request. Adapt the example to the actual schema; composite keys require complete ordering and continuation predicates. `truncated: true` means this SQL statement produced more rows than the response cap. Without a SQL `LIMIT`, it indicates whether to continue. With a SQL `LIMIT`, `truncated: false` only means that statement's result was not additionally truncated; more records may still exist. Separate requests do not share a database snapshot, so use a fixed business time window or business snapshot when data can change.

Managed MCP `query` / `raw_query` calls reuse the connection pool and validated rules for the same configuration version. Resources are released after one idle minute and reinitialized after five minutes of continuous use. New requests after configuration changes reinitialize; current identity and tool permissions are checked on every call, and query results are not cached. To investigate latency, correlate audit events by request ID and compare the start/end times of `query.initialize` (its `cache_hit` detail indicates reuse), `sql.rewrite`, `sql.guard`, `sql.execute` and `mcp.request`. Events can be nested, so do not simply add their durations.

HTTP MCP reuses validated configuration snapshots and authentication policies within one request, avoiding repeated full YAML reads for the same source. Endpoints whose path is the database ID locate that source directly; custom paths still validate the configured route and do not retain the old ID as an alias. Snapshots are never shared across requests: an in-flight request uses the configuration it has read, while new requests reread configuration and verify tokens and permissions. Saved disablement, permission revocation and route changes take effect on subsequent requests. Configuration writes within a request disable snapshot reuse. This does not cache business query results or retain an HTTP snapshot for the next call on a long-lived transport.

Enable the required tools in MCP configuration and save. Configure the client using the displayed connection information and the database's business authentication method. List the tools before sending a test request.

For clients that support connector packages, enable MCP, save and download the connector ZIP. Import it in the client and authenticate for the selected database.

For queries, enable and call `list_tables`, `describe_table` and `query` in order. For actions, use `list_actions`, `describe_action` and `invoke_action`. If a tool or resource is missing, check tool settings, publication status and the account's capabilities for the selected database.

## Audit logs and troubleshooting

Select a database or platform logs on the audit page. Narrow the results by request ID, operator, business identity or tool. For a failed call, check its database, identity, tool, policy version and outcome.

To diagnose HTTP MCP `ping` or shared-path latency, have an operator set `DATASCOPE_MCP_TIMING=1` in the MCP process environment and restart the service. It is off by default; remove the variable and restart after diagnosis. Search the service process logs for `mcp.request.timing` and correlate `request_id` with audit logs. Each POST request produces one summary with `method`, `tool`, `query_cache_hit`, HTTP `status`, `total_ms`, `first_write_ms`, `stages_ms` and `stage_counts`, without headers, tokens, SQL or business results. `first_write_ms` measures the application's first response-header write, not client time to first byte; it is null if no response was written. `method` may be empty for requests rejected before method parsing.

`route_lookup` measures endpoint configuration lookup; `resolve_auth`, authentication policy resolution; `handler_build`, protocol handler construction; `verify_token`, token verification; `middleware_get`, the protocol middleware's configuration reread; and `business_get`, the business-permission middleware's configuration read. `config_state`, `read_state`, `lock_wait` and `lock_hold` measure configuration operations, configuration file reads, file-lock waiting and lock holding. OneID also records `jwks_lock_wait` and, on a public-key cache miss, `jwks_fetch`. All stages use milliseconds; repeated stages accumulate, with invocation counts in `stage_counts`. Stages can overlap through nesting and must not be added together. Compare serial and four-concurrent `ping` requests: growing `lock_wait` suggests configuration lock contention; low application time with high client latency calls for comparison of the internal application port and public endpoint to locate gateway or network waiting. Timing is diagnostic only and does not change authentication, caching or locking policies.

Tool initialization also records `query_initialize`, covering query-service cache acquisition or creation. `query_cache_hit=true` means service reuse; false means no reuse; null (possibly `<nil>` in text logs) means this initialization step did not run. `tool` identifies calls such as `query`, `list_tables` and `describe_table`. On creation, `store_open` includes credential loading, connection opening and store configuration, while `service_initialize` measures query-rule and service initialization. For network databases, `connection_open` contains `connection_ping` (connectivity check), `connection_readonly` (beginning and rolling back a read-only transaction) and `connection_table_names` (initial table-directory discovery). Do not add these child stages to their parent; failures record only stages actually reached.

`query`, `raw_query`, `list_tables` and `describe_table` share a service cache keyed by the configuration snapshot. Services are rebuilt after one idle minute or five minutes of age; the next call after a configuration change uses a new service. Authentication and tool permissions are checked on every request. Metadata results are read on each call, without caching tool responses. Network database initialization validates live readability and column names with zero-row read-only queries, recorded as `validation_columns`, instead of fetching full indexes, foreign keys and comments for column validation. SQLite retains its existing schema validation.

`metadata_tables` and `metadata_describe` measure table-list and table-schema reads. They can occur during both service initialization and tool execution, so interpret their counts alongside parent stages. `query_execute` measures the database execution path, including connection/transaction acquisition, querying and reading results; it is not pure database SQL execution time. For slow first queries or metadata calls, compare `query_initialize`, then inspect `store_open`, `connection_open`, `service_initialize` and metadata reads. Cache hits normally omit creation stages. `query_release` measures service-lease release or connection closing.

- **Missing menu item** — Check the selected database and platform account grants.
- **Unexpected empty query** — Verify the parameters, business identity and row filters in the rewritten SQL.
- **No change after a dry run** — Dry runs roll back. Check the real call and its confirmation status in the action workbench.
