# Revizor — Debugging Without `print()` and `console.log()`

**End-user distribution.** For developers building Revizor from source — see [DEV_README.md](DEV_README.md).

## What is Revizor

You spend hours debugging. You add `print()`, restart the server, forget to remove it — code gets messy.

Revizor does it differently. You run a single binary. Your AI agent reads trace logs and finds the bug. You review the result.

**Revizor is AI-managed debug logging.** You give your AI agent a task. The agent reads the event stream and explains what happened.

## What Revizor Looks Like (60 Seconds)

Your Google OAuth login is broken. User clicks "Sign In", goes through the redirect — and ends up unauthenticated.

```bash
# 1. Revizor is already running (revizor serve)
# 2. AI agent reads logs — NO session needed, NO server restart:
curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"trace_search","arguments":{"search":"auth.","lines":50}}}'
```

```
[
  {"trace_path": "auth.callback.enter", "data": {"provider": "google"}},
  {"trace_path": "auth.callback.fetch_profile", "data": null},
  {"trace_path": "auth.callback.decision", "data": {"existing_link": null}},
  {"trace_path": "auth.create_identity.enter", "data": {"user_id": 42}},
  {"trace_path": "auth.create_session.success", "data": null},
  {"trace_path": "auth.redirect.success", "data": null},
  ❌ No auth.frontend.token_saved  ← this is where it breaks!
]
```

The AI agent reports: "Token is passed in the URL, but the frontend never saves it to localStorage. Check `auth/success/page.tsx:51`." You open the file — bug found. Less than a minute.

---

## For the AI Agent

> The full debugging guide is embedded in the binary. Start with `trace_ping` to check connectivity, then call **`trace_get_instructions`** for the complete reference — startup protocol, recipes, patterns, and best practices. No file access required.

---

## Installation & Startup

### Install from distribution

```bash
cd ais_revizor && sudo ./install.sh
# Installs binary to /usr/local/bin/ais_tools/revizor (symlink /usr/local/bin/revizor)
# Config to ~/.config/ais_tools/revizor/
```

### Start

```bash
# From project root (required for trace_inject/trace_audit to find source files)
cd /path/to/your/project
revizor serve

# Or explicitly:
/usr/local/bin/revizor serve
```

Starts instantly. HTTP server on port 9876. Database `revizor.db` created automatically in the current directory.

### Stop

```bash
# Graceful (flush buffers, close DB):
curl -s -X POST http://127.0.0.1:9876/api/v1/trace/shutdown

# Or Ctrl+C in the terminal where revizor serve is running
```

### Revizor with Docker

If your application runs in a Docker container and Revizor is on the host:

1. **Bind to all interfaces.** In `revizor.yaml`:
   ```yaml
   server:
     host: "0.0.0.0"
     port: 9876
   ```
   The default `host: "127.0.0.1"` prevents container access.

2. **Pass the URL to the container.** In `docker-compose.yml`:
   ```yaml
   services:
     backend:
       environment:
         REVIZOR_URL: http://host.docker.internal:9876
   ```
   `host.docker.internal` resolves to the host machine from inside a container.

3. **Create a trace session** (once at startup):
   ```bash
   curl -s -X POST http://127.0.0.1:9876/mcp \
     -H 'Content-Type: application/json' \
     -d '{"jsonrpc":"2.0","id":1,"method":"tools/call",
          "params":{"name":"trace_start",
                    "arguments":{"paths":"*","description":"app debugging"}}}'
   ```
   Save the `session_id` from the response and pass it to the container via `REVIZOR_SESSION_ID`.

4. **The SDK automatically passes `session_id`** via the `X-Revizor-Session` HTTP header. Make sure you have an up-to-date SDK version (v0.1.1+).

---

## Four Ways to Use Revizor

Revizor provides four interfaces — choose the one that fits your AI model.

| Method | Best for | How |
|--------|----------|-----|
| **MCP (direct)** | AI agents with tool_calls (Claude Code, Gemini, Cursor) | Add binary to MCP host config |
| **CLI wrapper** | AI agents WITHOUT tool_calls (DeepSeek, local models) | `python3 mcp_client_cli_0_1_0.py --server './revizor --mcp'` |
| **HTTP JSON-RPC** | Scripts, curl, CI/CD, any HTTP client | `curl -X POST http://127.0.0.1:9876/mcp` |
| **Prompt adapter** | LLMs without function calling (text protocol) | `./revizor --mcp` + `mcp_client_prompt_0_1_0` |

### Method 1: MCP Direct (Claude Code, Gemini, Cursor)

Add the binary to your MCP host configuration:

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

### Method 2: CLI Wrapper (DeepSeek, local models)

For models that cannot make direct MCP calls:

```bash
CLI="python3 modules/mcp_client_cli_0_1_0/mcp_client_cli_0_1_0.py --server './revizor --mcp'"

# Test connection
$CLI --tool trace_ping

# Search logs
$CLI --tool trace_search --args '{"search": "elevation", "lines": 50}'

# Read logs
$CLI --tool trace_read --args '{"lines": 100}'

# End session
$CLI --tool trace_expire --args '{"session_id": "session-uuid"}'
```

> **Note:** The CLI launches the binary in `--mcp` mode (stdio JSON-RPC). If the binary is already running as an HTTP server (`revizor serve`), use method 3 (HTTP JSON-RPC via curl) instead.

### Method 3: HTTP JSON-RPC (curl, scripts, CI/CD)

Binary is running as `revizor serve` on port 9876. All tools available via `POST /mcp` (full list: [DEV_README.md](DEV_README.md)).

**Ping-first pattern (REV-011):** Before any Revizor call, verify the binary is alive. If `trace_ping` fails, stop — the binary is not running.

```bash
# ALWAYS first: check connection
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

After ping succeeds, proceed with your debugging:

```bash
# Search logs by keyword (NO session needed)
curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"trace_search","arguments":{"search":"elevation","lines":50}}}'

# Search with path filter
curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"trace_search","arguments":{"search":"callback","path_filter":"auth.*","lines":30}}}'

# Read last 100 log lines (NO session needed)
curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"trace_read","arguments":{"lines":100}}}'

# Create a session (optional — only for time-range filtering)
curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"trace_start","arguments":{"paths":"elevation.**,auth.callback.*","description":"debug elevation"}}}'

# Read session logs
curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"trace_read","arguments":{"session_id":"session-uuid","lines":100}}}'

# End session
curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"trace_expire","arguments":{"session_id":"session-uuid"}}}'

# List active sessions
curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"trace_list_sessions","arguments":{}}}'

# List all tools
curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":9,"method":"tools/list","params":{}}'
```

> **Note on `trace_start`:** On large databases (hundreds of thousands of events), creating a session may take tens of seconds due to FTS indexing. HTTP clients with short timeouts may not receive a response — the session is still created in the database. Use `trace_search` and `trace_read` without `session_id` for instant log access.

### Method 4: Prompt Adapter (text protocol)

For LLMs that support neither MCP nor function calling. The adapter converts tools into text instructions. See `mcp_client_prompt_0_1_0` for details.

---

## Presets

Ready-made trace path sets for common scenarios:

```bash
# List presets
curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"trace_presets_list","arguments":{}}}'

# Create session with a preset
curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"trace_start","arguments":{"paths":"debug_elevation","description":"debug elevation"}}}'
```

---

## Sending Events to Revizor (SDK)

If you want your code to send trace events to the binary:

**Python:**
```python
from revizor_sdk import configure, trace
configure(endpoint="http://localhost:9876")
trace("my_module.my_function.enter", {"key": "value"})
```

**TypeScript:**
```typescript
import { initTrace, trace } from '@ais-platform/revizor-sdk';
initTrace({ apiBaseUrl: 'http://localhost:9876' });
trace('my_module.my_function.enter', { key: 'value' });
```

---

## All Tools (34 public + 1 internal = 35 total)

Full specification: [DEV_README.md](DEV_README.md).

| # | Tool | Description |
|---|------|-------------|
| 1 | `trace_ping` | Safe connectivity test. Returns `"pong"` without side effects. **Always call first.** |
| 2 | `trace_get_instructions` | Return the full AI agent debugging guide embedded in the binary. **Use after ping to get the complete reference. No file access required.** |
| 3 | `trace_read` | Read last N log lines (`session_id` optional). Use with `format: "table"` for human-readable output. |
| 4 | `trace_search` | Full-text search with optional `path_filter`, `data_filter` (REV-008, `key=value` or `key`), `count_by` (REV-009, `path` for aggregation), `context_lines` (REV-010, grep -C style). **Primary debugging tool.** |
| 5 | `trace_config` | Show currently active trace points for a session. |
| 6 | `trace_tail` | Streaming read: show new log entries since last read (cursor-based). |
| 7 | `trace_start` | Create a debug session and enable trace points by preset name or path list. |
| 8 | `trace_enable` | Enable a single trace point. Supports glob patterns (`auth.**`). |
| 9 | `trace_disable` | Disable a single trace point. |
| 10 | `trace_expire` | Force-expire a session: disable all its points and set expiration to now. |
| 11 | `trace_kill` | Kill one or all active trace sessions — force expire with cleanup. |
| 12 | `trace_validate_path` | Check if a trace path format is valid (lowercase, `[a-z0-9_.]+`, max 120 chars). **Use before every commit.** |
| 13 | `trace_why` | Explain why a trace point is enabled or disabled — checks all glob rules. |
| 14 | `trace_list_sessions` | Show all active trace sessions. |
| 15 | `trace_presets_list` | List available presets (from DB, fallback to YAML). |
| 16 | `trace_preset_set` | Create or update a preset in the database. Supports `!` exclude paths. |
| 17 | `trace_stats` | Show session metrics: total events, unique paths, last event timestamp. |
| 18 | `trace_list_points` | Show all known trace paths in the system, grouped by module. |
| 19 | `trace_verify_coverage` | Verify chain integrity: each `.enter` must have `.success` or `.failed`. |
| 20 | `trace_suggest_coverage` | Compare registered paths against expected `.enter`/`.success`/`.failed` pattern. |
| 21 | `trace_diff` | Compare two sessions: which trace paths differ. |
| 22 | `trace_inject` | Insert `trace()` calls into Python/TypeScript source files. Supports `dry_run`. |
| 23 | `trace_targets` | Scan project filesystem to prioritize files for trace injection (P1–P5). |
| 24 | `trace_audit` | Audit Python/TypeScript modules for RE-033 compliance. **Use before every commit.** |
| 25 | `trace_cost` | Show historical frequency of a trace point (events/minute over a time period). |
| 26 | `trace_generate_test` | Generate a pytest test from the event chain of a single `request_id`. |
| 27 | `trace_session_summary` | Session overview: event count, unique paths, last event time, status. |
| 28 | `trace_orchestrator_events` | Timeline of trace events for an orchestrator task (filters by `task_id`). |
| 29 | `trace_shutdown` | Gracefully stop the Revizor process (flush buffers, close DB, exit cleanly). |
| 30 | `trace_license_renew` | Manual license renewal by the agent (replaces heartbeat). |
| 31 | `trace_remove` | Remove `trace()` calls from source code (reverse of trace_inject). |
| 32 | `trace_webhook_list` | List registered webhook notifications and their delivery status (REV-W-001). |
| 33 | `trace_webhook_test` | Send a test ping to a specific webhook (REV-W-001). |


> **Internal diagnostic tool:** `trace_debug_log` — writes MCP request/response to a debug log file. Not listed in the public API; used only for diagnosing Revizor itself.

---

## Webhook Notifications (REV-W-001)

Revizor can notify external systems when specific trace events occur. Use this to invalidate stale verdicts in AIS Surveyor.

Configure in `revizor.yaml`:

```yaml
webhooks:
  - id: "surveyor-invalidation"
    url: "http://localhost:9877/api/v1/revizor-events"
    path_filter: "auth.**,payment.**"
    enabled: true
```

Delivery is async (goroutine), with retry: 3 attempts at 5-second intervals. Failed deliveries after all retries are logged at CRITICAL level.

**MCP tools:**
- `trace_webhook_list` — list all webhooks with delivery status
- `trace_webhook_test` — send a test ping to a specific webhook

```bash
# List registered webhooks
curl -s -X POST http://127.0.0.1:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"trace_webhook_list","arguments":{}}}'

# Test a webhook
curl -s -X POST http://127.0.0.1:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"trace_webhook_test","arguments":{"webhook_id":"surveyor-invalidation"}}}'
```

## Licensing

### Tier Comparison

| Tier | Sessions | Events/day | Machines | Tools | Storage | Max Version |
|------|----------|-----------|----------|-------|---------|-------------|
| **Community** (no key) | 1 | 100 | 1 | Basic (3) | SQLite | — |
| **Trial** | ∞ | ∞ | 3 | Full | SQLite | — |
| **Indie** | 5 | 10K | 1 | Full | SQLite | — |
| **Pro** | 10 | 100K | 3 | Full | **PostgreSQL** | per license |
| **Enterprise** | ∞ | ∞ | ∞ | Full | **PostgreSQL** | per license |
| **Privileged** | ∞ | ∞ | Custom | Full | **PostgreSQL** | per license |

- **Storage:** Pro/Enterprise/Privileged licenses can use PostgreSQL instead of SQLite. Requires `postgres` feature in the license key and `storage.type: postgres` in `revizor.yaml` (v58).
- **Max Version:** Perpetual licenses (`exp=-1`) may have `max_ver` field — the binary version must not exceed this value, or the license falls back to Community mode (v58).
- **Sunday Unlimited** — free unlimited usage every Sunday for all tiers.
- **Activation:** license key is embedded in your downloaded binary, or set `license_key` in `revizor.yaml`.

---

## Configuration

Config file: `revizor.yaml` (searched in CWD, then `~/.config/ais_tools/revizor/`, then `~/.config/revizor/` legacy). Template: `revizor.yaml.example`.

```yaml
server:
  host: "127.0.0.1"
  port: 9876

storage:
  type: sqlite                 # or postgres
  sqlite_path: "./revizor.db"

logging:
  level: info                  # debug | info | warn | error

presets:                       # ready-made path sets
  debug_elevation:
    description: "Debug elevation flow"
    paths:
      - "elevation.**"
      - "auth.callback.*"
```

---

## Detailed Documentation

- [DEV_README.md](DEV_README.md) — developer guide (build from source, MCP stdio, SDK, architecture, full tool reference)
- [docs/API.md](docs/API.md) — full HTTP API specification
- [passport.md](passport.md) — technical module passport
