# Local task failure timeline

Status: First implementation. Linux only. This branch depends on the condition-waits branch and PR #54.

Call `task_timeline` with `task_id`. It returns events, source availability, a local artifact path, and a continuation cursor. The default page has at most 50 events. The maximum is 200.

```json
{"task_id":"<task id>","limit":50}
```

Pass the returned `next` object as `after` to continue. A cursor belongs to one task. `gap: true` means earlier events were evicted. `dropped` reports journal eviction. An optional `action_id` filters explicit action links. It does not claim that nearby requests or console entries were caused by that action.

## Sources

- Tool calls retain task and screen-instance identity, action IDs, errors, and condition outcomes when they return. A screen with a task records that task ID in its registry entry.
- Failed calls and wait timeouts attempt a small page excerpt immediately after the call. The excerpt contains up to 20 element names, roles, and states. It omits input values. A failed source read is reported as unavailable.
- Native-app DevTools requests and console entries are copied from the diagnostics collector at tool boundaries and timeline queries. Bare excerpts are copied only from explicit `browser_console` and `browser_network` replies. Timeline collection does not call those tools because their reads advance native cursors. The collector is not reconnected to manufacture missing history. Request bodies, headers, and cookies are not collected.
- Owned process logs contribute a tail excerpt of at most 4096 bytes. The full bounded, redacted log remains linked. Retained process exit results appear as separate events.

Events have a task-local collection sequence, an observation time, and a source time when available. Collection order and nearby source timestamps do not prove causality. Process log modification time describes the file, not each log line.

## Storage and privacy

The journal is `timeline.json` inside the task directory. It survives task closure and uses the task's seven-day artifact retention policy. It holds at most 512 events. Each event payload is limited to 8192 bytes. Oversized payloads get an explicit truncation marker. Repeated reads use per-stream sequence positions to avoid replaying old collector buffers after journal eviction.

Evidence is redacted before persistence. Typed text is already reduced by the action audit. URL queries, known token formats, and known secret fields are removed. Pattern redaction cannot remove every possible secret from arbitrary logs or element names. Keep secrets out of fixture output. Evidence remains local; the tool does not upload it.

Sources report available, partial, or unavailable. Lost browser events, log excerpts, disconnected collectors, and missing files remain visible as limits. A journal write failure adds a warning to a tool response when that response exists. It does not replace the original result. A crash or failed write can leave an incomplete journal.

## Current limits

- Screenshots are not collected. Failure page excerpts provide text evidence only.
- Collection occurs at tool boundaries and queries. It is not continuous process or browser tracing.
- Diagnostic buffers can expire between calls. Closing a browser removes its live collector. Events already copied to the task journal remain available.
- Bare browser outcomes and explicitly requested native console/network text excerpts are retained. These excerpts are marked partial because the native reports do not expose stable request-start cursors or exact loss counts. Native-app DevTools diagnostics retain structured events. Standalone condition cursors remain native-app tools; use Bare step assertions for browser postconditions.
- Native graphical app stdout logs are not imported. Process excerpts come from `task_launch` resources.

## Validation

Storage tests cover redaction, cross-task cursor refusal, pagination, deduplication, journal gaps, and preservation of existing evidence when an atomic write fails. A controlled browser fixture verifies that one timeline response contains an action, HTTP 500, console error, process output and exit, wait failure, and page excerpt. It also verifies explicit action links and evidence retention after task close.

A native-app Chrome cage journey passed once on `9c44623`, before the Bare-default merge. It verified server survival after screen creation, the HTTP 500 journey, timeline contents, redaction, pagination, action links, resource cleanup, and journal retention. Screenshots, mirror visuals, concurrency, and the MCP response warning after a write failure remain untested.

A live Bare cage journey passed once on `3eea517`. It verified mapped Bare, server survival, native assertion success and failure, non-consuming timeline reads, explicit diagnostic retention, failure outlines with values omitted, redaction, pagination, cleanup, and journal retention. One session and one screen were tested. Mirror visuals and concurrency were not tested.
