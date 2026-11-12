# DataScope user guide

## Getting started

Choose a database in the top bar. Your platform permissions for that database determine which pages appear. Open the SQL workbench to query data or the action workbench to run a configured operation.

When connecting a database for the first time, add and test the connection, expose one table in query configuration, and run a query as a business user. Verify the visible rows and fields before enabling tools for a client.

## Databases, accounts and settings

Administrators can register a network database, connect or create a SQLite database, or upload a SQLite or CSV file up to 20 MiB. Use UTF-8 for CSV. The first row supplies column names and values are imported as text into a table named `data`.

For a network database, configure the schema scope before enrolling tables. When cloning configuration, check the source, destination and preview before applying changes.

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

The platform management connector uses `/mcp/admin`, separately from each database's business MCP. Management MCP supports SQLite DDL object inspection, preview, execution and history when DDL management is enabled and permissions allow it. Preview to obtain a plan_id before applying; check execution records when the result is uncertain. Business MCP does not expose DDL. System administrators can also test and save candidate network connections or remove a data source registration. Removal preserves business files; connection editing cannot change the target database.

The action AI assistant follows the current draft entry point: main uses db.session() and CommonJS; legacy run retains tagged SQL templates without automatic migration. Both contracts reject new or changed policy-bypassing SELECT statements relative to the saved action. Author and review any new reads manually.

Enable the required tools in MCP configuration and save. Configure the client using the displayed connection information and the database's business authentication method. List the tools before sending a test request.

For clients that support connector packages, enable MCP, save and download the connector ZIP. Import it in the client and authenticate for the selected database.

For queries, enable and call `list_tables`, `describe_table` and `query` in order. For actions, use `list_actions`, `describe_action` and `invoke_action`. If a tool or resource is missing, check tool settings, publication status and the account's capabilities for the selected database.

## Audit logs and troubleshooting

Select a database or platform logs on the audit page. Narrow the results by request ID, operator, business identity or tool. For a failed call, check its database, identity, tool, policy version and outcome.

- **Missing menu item** — Check the selected database and platform account grants.
- **Unexpected empty query** — Verify the parameters, business identity and row filters in the rewritten SQL.
- **No change after a dry run** — Dry runs roll back. Check the real call and its confirmation status in the action workbench.
