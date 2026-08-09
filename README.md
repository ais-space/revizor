# AIS Revizor — Runtime Trace Engine for AI Coding Agents

**Give your AI coding agent X-Ray vision.**

AIS Revizor is a runtime trace engine that turns execution into causal chains
your agent can inspect in milliseconds. Trace points placed once. Off by default.
O(1) — a dict lookup, nothing more. The agent runs `trace_search` and sees the
full causal chain — `.enter` without `.success` is the breakage point. No
reconstruction. No guessing.

Traditional observability tools were designed for engineers. AIS Revizor was
designed for AI agents.

## Key Features

- **Causal chains, not log lines.** Agent sees `.enter` → `.success` / `.failed`
  patterns, instantly locates breakage points without reconstructing execution.
- **Zero-session search.** Agent searches without creating sessions, without
  server restarts. One query, answer in milliseconds.
- **Built for the agent.** MCP interface (30 tools) designed for AI agents to
  manage debugging autonomously.
- **Single binary (~15 MB).** Embedded SQLite. No cloud dependencies. Your code
  never leaves your perimeter.
- **Two-layer PII sanitization.** Passwords, tokens, API keys, emails never
  reach the log. Automatic. No configuration.
- **Graceful degradation.** `trace()` failure never crashes the application.
  O(1) no-op when disabled. Zero production risk.
- **Four access modes.** MCP direct (Claude/Gemini), CLI wrapper (DeepSeek),
  HTTP JSON-RPC (CI/CD), Prompt adapter (any LLM).

## Repository Structure

```
revizor/
├── v0.1.0/            # AIS Revizor 0.1.0 — complete source code
│   ├── LICENSE        # FSL-1.1-ALv2
│   ├── cmd/           # CLI entry points
│   ├── internal/      # Core implementation
│   ├── sdk/           # Python and TypeScript SDKs
│   ├── docs/          # Documentation
│   └── README.md      # Version-specific readme
├── LICENSING.md       # Licensing and source-publication policy
└── README.md          # This file
```

## Licensing

Each published version of AIS Revizor is licensed under the **Functional Source
License, Version 1.1, ALv2 Future License** (FSL-1.1-ALv2).

The Change Date for each published version is two years from its publication
date. After the Change Date, the version is additionally available under
Apache License 2.0.

See [LICENSING.md](LICENSING.md) for the full licensing and source-publication
policy.

## Getting Started

```bash
# Download and install
curl -sL https://ais-platform.dev/dl/revizor -o install.sh
chmod +x install.sh && ./install.sh

# Start the server
revizor serve

# Read the documentation
cat docs/README.md
```

## Links

- [AIS Platform](https://ais-platform.dev) — official website
- [Revizor Documentation](https://ais-platform.dev/revizor)
- [Revizor Pricing](https://ais-platform.dev/revizor/pricing)
- [Support](mailto:support@ais-platform.dev)

---

AIS Revizor is a product of [AIS Platform](https://ais-platform.dev).
