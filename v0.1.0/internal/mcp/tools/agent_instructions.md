
# Revizor AI Agent Instructions

> This guide is embedded in the binary. Call `trace_get_instructions` at any time to re-read it. No file access required.

## Startup Protocol (Always)

1. **Call `trace_ping` first.** If `"pong"` — binary is alive, proceed. If connection refused — the binary is not running. Stop. Do not call any other tools.
2. **Call `trace_get_instructions` if you need the full reference.** This entire guide is always available through MCP.
3. **Read logs via `trace_search`.** `session_id` is optional. This is your primary debugging tool — no session creation required.
4. **Before debugging new code:** `trace_audit` to verify trace points exist. If missing — `trace_inject` with `dry_run: true` to preview, then without `dry_run` to apply.
5. **Validate paths after adding:** `trace_validate_path` on every new path. Format: `[a-z0-9_.]+`, max 120 chars, snake_case.
6. **When done:** `trace_expire` to clean up sessions. `trace_shutdown` for graceful process stop.

## Core Methodology: The Golden Rule

**Look for `.enter` without a matching `.success` — that's the breakage point.**

Every operation has an `.enter` at the start and a `.success` (or `.failed`) at the end. A missing `.success` means the operation didn't finish — that's your bug.

## How to Read Trace Logs

Each entry is an event with path `module.component.event`:

| Pattern | Meaning | Action |
|---------|---------|--------|
| `.enter` → `.success` | Operation completed | Move on |
| `.enter` without `.success` | Operation didn't finish | **This is the breakage point** |
| `.enter` → `.failed` | Operation threw an exception | Read `data.reason` for the error message |
| `.decision` | Branch point | Shows which condition was taken |
| Repeated `.enter` for same path | Loop or retry | Check the rate — may indicate a problem |
| `.success` with `error` field non-null | Partial success (best-effort) | Operation completed but a sub-step failed — check the error |

## Recipes

### Recipe 1: Localize a crash in a known flow

Search by module name, look for `.enter` without `.success`.

```bash
curl -s -X POST http://127.0.0.1:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call",
       "params":{"name":"trace_search","arguments":{"search":"module_name.","lines":20}}}'
```

### Recipe 2: Verify a full end-to-end flow

Search for all entry points, count matching `.success`/`.failed` pairs.

```bash
curl -s -X POST http://127.0.0.1:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call",
       "params":{"name":"trace_search","arguments":{"search":".enter","path_filter":"module.*","lines":30}}}'
```

### Recipe 3: Three-layer audit

1. **trace_search** — confirms WHAT happened and WHERE
2. **SQL** — gives EXACT values (amounts, balances, counts)
3. **Source code** — explains WHY if the above two don't

### Recipe 4: Before-and-after fix verification

Run the SAME `trace_search` before and after the fix. The difference is objective proof.

### Recipe 5: Precise data search

```bash
# Events where field equals value
curl ... trace_search '{"search":"","data_filter":"license_ok=false","lines":20}'

# Events where field exists (any value)
curl ... trace_search '{"search":"","data_filter":"license_ok","lines":50}'

# Aggregate by path
curl ... trace_search '{"search":"","data_filter":"license_ok=false","count_by":"path"}'

# Context around matches (grep -C style)
curl ... trace_search '{"search":"order_id","context_lines":5,"lines":20}'
```

### Recipe 6: Compliance check before commit

```bash
# Audit modules for RE-033 trace import
curl ... trace_audit '{"module_path": "modules/my_module_0_1_0"}'

# Validate all trace paths in a file
grep -oP 'trace\("[^"]+"' file.py | cut -d'"' -f2 | while read path; do
  curl ... trace_validate_path "{\"path\": \"$path\"}"
done
```

## Best Practices

1. **Always ping before searching.** `trace_ping` confirms the binary is alive.
2. **Search without session_id is your default.** Sessions are only for time-range filtering (`since`/`until`).
3. **Search by module name as entry point.** Don't guess the exact path — search broadly, then drill down.
4. **Combine `trace_search` + `trace_read`.** Search finds the area, read gives full context.
5. **Compare before and after every fix.** Same `trace_search` — difference is proof.
6. **Use `trace_inject --dry-run` for code verification.** Shows file as unified diff.
7. **Use `trace_audit` before every commit.** Checks RE-033 compliance.
8. **Use `trace_validate_path` on every new path.** No exceptions.

## Ping-First Pattern (use this in scripts)

```bash
PING=$(curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"trace_ping","arguments":{}}}')

if echo "$PING" | grep -q '"text.*pong'; then
    echo "Revizor is alive, proceed."
else
    echo "Revizor is NOT running. Start it with: /usr/local/bin/revizor serve" >&2
    exit 1
fi
```

## Important: trace_start on large databases

On databases with hundreds of thousands of events, `trace_start` may take tens of seconds due to FTS indexing. The session is created in the database even if the HTTP client times out. **Use `trace_search` and `trace_read` without `session_id` for instant log access** — sessions are not required for reading.

## Graceful Shutdown

To stop the Revizor process cleanly (flush buffers, close DB):

```bash
curl -s -X POST http://127.0.0.1:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"trace_shutdown","arguments":{}}}'
```

## Complete Tool Reference

| # | Tool | Category | Purpose |
|---|------|----------|---------|
| 1 | `trace_ping` | Diagnostic | Connectivity test with license status. **Always call first.** |
| 2 | `trace_get_instructions` | Diagnostic | Return this guide. Embedded in binary — no file access needed. |
| 3 | `trace_license_renew` | Diagnostic | Manual license renewal (replaces heartbeat). |
| 4 | `trace_read` | Log Reading | Read last N log lines. `session_id` optional. |
| 5 | `trace_search` | Log Reading | Full-text search with `path_filter`, `data_filter`, `count_by`, `context_lines`. **Primary tool.** |
| 6 | `trace_config` | Log Reading | Show active trace points for a session. |
| 7 | `trace_tail` | Log Reading | Streaming read: new events since last read (cursor-based). |
| 8 | `trace_start` | Sessions | Create debug session and enable trace points by preset or path list. |
| 9 | `trace_enable` | Sessions | Enable a single point. Supports glob patterns (`auth.**`). |
| 10 | `trace_disable` | Sessions | Disable a single point. |
| 11 | `trace_expire` | Sessions | Force-expire a session: disable all points, set expiry to now. |
| 12 | `trace_kill` | Sessions | Kill one or all active sessions with cleanup. |
| 13 | `trace_validate_path` | Stats | Validate path format (`[a-z0-9_.]+`, max 120 chars). |
| 14 | `trace_why` | Stats | Explain why a point is enabled/disabled — checks all glob rules. |
| 15 | `trace_list_sessions` | Stats | List all active sessions. |
| 16 | `trace_presets_list` | Stats | List available presets (from DB, fallback to YAML). |
| 17 | `trace_preset_set` | Stats | Create or update a preset in the database. Supports `!` excludes. |
| 18 | `trace_stats` | Stats | Session metrics: total events, unique paths, last event time. |
| 19 | `trace_list_points` | Analysis | All known trace paths, grouped by module. |
| 20 | `trace_verify_coverage` | Analysis | Check chain integrity: each `.enter` must have `.success`/`.failed`. |
| 21 | `trace_diff` | Analysis | Compare two sessions: which trace paths differ. |
| 22 | `trace_cost` | Generation | Historical frequency of a trace point (events/minute). |
| 23 | `trace_generate_test` | Generation | Generate a pytest test from the event chain of a `request_id`. |
| 24 | `trace_suggest_coverage` | Generation | Compare registered paths against expected `.enter`/`.success`/`.failed` pattern. |
| 25 | `trace_inject` | Instrumentation | Insert `trace()` calls into Python/TypeScript. dry_run, .bak. |
| 26 | `trace_remove` | Instrumentation | Remove `trace()` calls (reverse of trace_inject). |
| 27 | `trace_targets` | Instrumentation | Scan filesystem to prioritize files for injection (P1–P5). |
| 28 | `trace_audit` | Instrumentation | Audit modules for RE-033 compliance. **Use before commit.** |
| 29 | `trace_session_summary` | Orchestrator | Session overview: events, unique paths, last event, status. |
| 30 | `trace_orchestrator_events` | Orchestrator | Timeline of events for an orchestrator task (filter by `task_id`). |
| 31 | `trace_shutdown` | Process | Graceful stop: flush buffers, close DB, exit. |
| 32 | `trace_webhook_list` | Webhook | List registered webhooks and delivery status. |
| 33 | `trace_webhook_test` | Webhook | Test ping to a specific webhook. |
| 34 | `trace_update_check` | Diagnostic | Check for Revizor updates (manual + automatic at startup, v58). |

> **Internal:** `trace_debug_log` — toggle MCP request/response logging to a file. For Revizor diagnostics only.

---

## How Users Send Trace Events (SDK)

Your code must contain `trace()` calls for Revizor to see events. Use `trace_inject` to add them automatically, or the SDK manually:

**Python:**
```python
from revizor_sdk import configure, trace, trace_start

# One-time setup (reads REVIZOR_URL env var automatically)
configure(endpoint="http://localhost:9876")

# Fire-and-forget event
trace("my_module.my_function.enter", {"key": "value"})

# Measure duration
end = trace_start("my_module.operation")
# ... code ...
end({"result": "ok"})
# Sends: my_module.operation.enter → my_module.operation.success (with duration_ms)
```

**TypeScript:**
```typescript
import { initTrace, trace, traceStart } from '@ais-platform/revizor-sdk';

initTrace({ apiBaseUrl: 'http://localhost:9876' });

// Fire-and-forget event
trace('my_module.my_function.enter', { key: 'value' });

// Measure duration
const end = traceStart('my_module.operation');
// ... code ...
end({ result: 'ok' });
```

**Environment variables:**
- `REVIZOR_URL` — where the SDK sends events (default: `http://localhost:9876`)
- `REVIZOR_SESSION_ID` — session ID added to every event via `X-Revizor-Session` header

**SDK behavior:**
- **Fire-and-forget:** `trace()` never waits for server response
- **Never-throw:** exceptions are swallowed, your app never crashes because of tracing
- **Zero-overhead when disabled:** if a point is off, `trace()` returns instantly
- **Client-side sanitization:** passwords, tokens, emails are masked before sending
- **Batching:** events are queued and sent in batches (every 100ms or 50 events)

### Browser Console Noise (TypeScript SDK)

When Revizor is not running, the TypeScript SDK's background fetch requests fail silently — but the browser may still log connection errors to the console. To prevent console noise:

**1. Disable SDK entirely (recommended for production):**
```typescript
import { initTrace } from '@ais-platform/revizor-sdk';

// Disable all tracing — no network requests, no console noise
initTrace({ apiBaseUrl: '' });  // empty URL = auto-disable
```
When `apiBaseUrl` is empty, the SDK's `autoInit()` stops the flush timer and clears the buffer. No fetch requests are made.

**2. Disable only on production builds:**
```typescript
if (process.env.NODE_ENV === 'production') {
    initTrace({ apiBaseUrl: '' });  // disabled — no console noise
} else {
    initTrace({ apiBaseUrl: 'http://localhost:9876' });
}
```

**3. Python SDK (no browser — but same pattern):**
```python
from revizor_sdk import configure

# Disable when Revizor is not available
configure(enabled=False)

# Or conditionally
import os
configure(enabled=bool(os.getenv("REVIZOR_URL")))
```

**General rule:** always call `initTrace` with an empty URL (TypeScript) or `configure(enabled=False)` (Python) in environments where Revizor is not running. This prevents pointless connection attempts. The `trace()` function itself remains safe — it checks `enabled` first and returns immediately when disabled.

## Adding Trace Points to Code (trace_inject)

Use `trace_inject` to automatically insert `trace()` calls into source files. This is the primary way to add trace coverage.

```bash
# Preview: show unified diff without modifying the file
curl -s -X POST http://127.0.0.1:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call",
       "params":{"name":"trace_inject","arguments":{"file_path":"modules/my_module/file.py","dry_run":"true"}}}'

# Apply: insert trace calls and create .bak backup
curl -s -X POST http://127.0.0.1:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call",
       "params":{"name":"trace_inject","arguments":{"file_path":"modules/my_module/file.py","dry_run":"false"}}}'
```

**What `trace_inject` does:**
- Adds `import trace` statement if missing
- Inserts `trace("module.component.enter", {...})` at the start of every function
- Inserts `trace("module.component.success", {...})` before each return
- Inserts `trace("module.component.failed", {...})` before each raise/throw
- Module name is auto-detected from file path
- Language is auto-detected from file extension (.py → Python, .ts/.tsx → TypeScript)
- Creates `.bak` backup before writing

**Python specifics:**
- One-liner functions (`def f(x): return expr`) are automatically split into multi-line
- Nested functions, class methods, and async functions are all supported

**TypeScript specifics:**
- PascalCase component names are auto-converted to snake_case in trace paths
- `export function NavMenu()` → trace path `nav_menu.enter`

## Removing Trace Points from Code (trace_remove)

Reverse of `trace_inject` — removes all previously injected `trace()` calls:

```bash
curl -s -X POST http://127.0.0.1:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call",
       "params":{"name":"trace_remove","arguments":{"file_path":"modules/my_module/file.py","dry_run":"true"}}}'
```

Always preview with `dry_run: true` first.

## Scanning for Trace Coverage Gaps (trace_targets + trace_audit)

```bash
# Scan project to find files needing trace injection (P1 = critical, P5 = low priority)
curl -s -X POST http://127.0.0.1:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"trace_targets","arguments":{}}}'

# Audit a module for RE-033 compliance (checks if trace import exists)
curl -s -X POST http://127.0.0.1:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call",
       "params":{"name":"trace_audit","arguments":{"module_path":"modules/my_module_0_1_0"}}}'
```

## Using Presets

Presets are ready-made sets of trace paths for common debugging scenarios. List them with `trace_presets_list`, then pass a preset name to `trace_start` instead of listing paths manually.

```bash
# List available presets
curl -s -X POST http://127.0.0.1:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"trace_presets_list","arguments":{}}}'

# Create a session using a preset
curl -s -X POST http://127.0.0.1:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call",
       "params":{"name":"trace_start","arguments":{"paths":"debug_elevation","description":"debug flow"}}}'

# Create a session with explicit glob paths
curl -s -X POST http://127.0.0.1:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call",
       "params":{"name":"trace_start","arguments":{"paths":"auth.**,payment.*","description":"debug auth+payment"}}}'
```

## Streaming Logs (trace_tail)

For long-running debugging sessions, use `trace_tail` to stream new events as they arrive:

```bash
curl -s -X POST http://127.0.0.1:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"trace_tail","arguments":{}}}'
```

Returns new events since the last `trace_tail` call (cursor-based). First call returns the last N events.

## Comparing Sessions (trace_diff)

Compare two debugging sessions to see what changed between them:

```bash
curl -s -X POST http://127.0.0.1:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call",
       "params":{"name":"trace_diff","arguments":{"session_id_a":"uuid-1","session_id_b":"uuid-2"}}}'
```

Useful after a fix: session A = broken run, session B = fixed run.

## Generating Tests from Trace Events (trace_generate_test)

Generate a pytest test from a real request's event chain:

```bash
curl -s -X POST http://127.0.0.1:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call",
       "params":{"name":"trace_generate_test","arguments":{"request_id":"uuid"}}}'
```

## Orchestrator Events

When debugging AI orchestrator tasks (Foreman, Surveyor), filter by task ID:

```bash
curl -s -X POST http://127.0.0.1:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call",
       "params":{"name":"trace_orchestrator_events","arguments":{"task_id":"task-uuid"}}}'
```

## Webhook Notifications (REV-W-001)

Revizor can notify external systems when specific trace events occur. Configure in `revizor.yaml`:

```yaml
webhooks:
  - id: "surveyor-invalidation"
    url: "http://localhost:9877/api/v1/revizor-events"
    path_filter: "auth.**,payment.**"
    enabled: true
```

Delivery is async (goroutine), with 3 retries at 5-second intervals.

## Checking for Updates

```bash
# Manual check
curl -s -X POST http://127.0.0.1:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"trace_update_check","arguments":{}}}'
```

The binary also checks for updates automatically at startup and logs a WARN if a newer version is available.

## Four Ways to Access Revizor

If direct MCP is unavailable, there are three other ways to call Revizor tools. Explain these to your user when needed.

### Method 1: MCP Direct (you, Claude Code, Gemini, Cursor)

This is what you're using. The binary is registered in the MCP host config and exposes tools natively.

```json
{
  "mcpServers": {
    "revizor": {
      "command": "/usr/local/bin/ais_tools/revizor",
      "args": ["--mcp"]
    }
  }
}
```

### Method 2: HTTP JSON-RPC (curl, scripts, CI/CD)

The binary runs as `revizor serve` on port 9876. All 35 tools available via `POST /mcp`. This is the most universal method — works from any HTTP client, shell script, CI pipeline, or programming language.

```bash
curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"trace_ping","arguments":{}}}'
```

### Method 3: CLI Wrapper (AI agents WITHOUT tool_calls: DeepSeek, local models)

For models that cannot make direct MCP tool calls. The CLI wrapper launches the binary in `--mcp` (stdio JSON-RPC) mode and translates tool calls to command-line invocations.

```bash
CLI="python3 modules/mcp_client_cli_0_1_0/mcp_client_cli_0_1_0.py --server './revizor --mcp'"

# Test connection
$CLI --tool trace_ping

# Search logs
$CLI --tool trace_search --args '{"search": "elevation", "lines": 50}'

# Read last 100 lines
$CLI --tool trace_read --args '{"lines": 100}'

# End session
$CLI --tool trace_expire --args '{"session_id": "session-uuid"}'
```

> **Important:** the CLI wrapper only works with `--mcp` (stdio) mode, not with `revizor serve` (HTTP). If the binary is already running as an HTTP server, use Method 2 (HTTP JSON-RPC via curl).

### Method 4: Prompt Adapter (LLMs without ANY tool/function calling)

For models that support neither MCP tool calls nor function calling — only plain text prompts. The adapter converts Revizor tools into text instructions that the LLM can embed in its prompts.

**How it works:**
1. The binary is launched in `--mcp` (stdio) mode
2. The prompt adapter (`mcp_client_prompt_0_1_0`) acts as a bridge: it reads the LLM's text output, extracts Revizor commands, executes them against the binary, and injects the results back into the prompt
3. The LLM sees the results as text in its context window — no tool calling required

**Example — what the LLM outputs (text prompt):**
```
<revizor>
<tool>trace_ping</tool>
</revizor>
```

**The adapter executes this and injects back:**
```
<revizor_result>
pong (Community mode — no license, v0.1.0)
</revizor_result>
```

**Example — search logs:**
```
<revizor>
<tool>trace_search</tool>
<args>{"search": "auth.", "lines": 20}</args>
</revizor>
```

**Example — inject trace points into a file (with dry_run preview):**
```
<revizor>
<tool>trace_inject</tool>
<args>{"file_path": "modules/auth_oauth_0_1_0/auth_oauth_0_1_0.py", "dry_run": "true"}</args>
</revizor>
```

The adapter returns a unified diff showing where `trace()` calls would be inserted. To apply, change `"dry_run"` to `"false"`.

**The adapter returns the matching log entries as text in the prompt.**

This method is the least efficient (adds latency from text parsing) but is the only option for LLMs that can't call tools at all. Use it as a last resort.

> **Note:** the Prompt adapter and CLI wrapper both require `--mcp` (stdio) mode. They do NOT work with `revizor serve` (HTTP). For the HTTP server, use Method 2.

## Docker: Revizor + Containers

If the application runs in Docker but Revizor is on the host:

1. **Bind to all interfaces** in `revizor.yaml`: `host: "0.0.0.0"` (default `127.0.0.1` blocks containers)
2. **Pass URL to container** in `docker-compose.yml`: `REVIZOR_URL: http://host.docker.internal:9876`
3. **Create a session at startup** and pass `REVIZOR_SESSION_ID` to the container
4. **SDK auto-passes session_id** via `X-Revizor-Session` HTTP header (SDK v0.1.1+)

## Licensing

### Tier Comparison

| Tier | Sessions | Events/day | Machines | Tools | Storage | Max Version |
|------|----------|-----------|----------|-------|---------|-------------|
| **Community** (no key) | 1 | 100 | 1 | Basic (3) | SQLite | — |
| **Trial** | ∞ | ∞ | 3 | Full | SQLite | — |
| **Indie** | 5 | 10,000 | 1 | Full | SQLite | — |
| **Pro** | 10 | 100,000 | 3 | Full | **PostgreSQL** | per license |
| **Enterprise** | ∞ | ∞ | ∞ | Full | **PostgreSQL** | per license |
| **Privileged** | ∞ | ∞ | Custom | Full | **PostgreSQL** | per license |

### Key Rules

- **Sunday Unlimited:** all tiers get unlimited usage every Sunday — no session/event limits
- **Community mode** activates automatically when no license key is provided or the key is invalid
- **PostgreSQL** requires `postgres` feature in the license key + `storage.type: postgres` in `revizor.yaml`
- **Max Version:** perpetual licenses (`exp=-1`) may have `max_ver` — binary version must not exceed it
- **Activation:** first launch with a license key registers the machine. `trace_license_renew` sends heartbeats
- **Enterprise offline:** Enterprise licenses work without activation or heartbeats

### When the user hits a limit

| Symptom | Cause | Fix |
|---------|-------|-----|
| `trace_start` fails | Too many active sessions | `trace_kill` old sessions, or upgrade tier |
| Events not appearing | Daily event limit reached | Wait until next day (or Sunday), or upgrade tier |
| Tools return errors | Community mode (3 tools only) | Provide a license key |
