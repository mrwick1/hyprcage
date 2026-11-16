# Condition waits

Status: Process exits, element presence/absence, completed request outcomes, request cursors, and action expectations are implemented. Request observation uses the current diagnostics collector. A browser without that capability returns unsupported. The separate bare integration is unchanged.

The Linux tool `task_wait` waits for a process resource returned by `task_launch`. It runs outside the global screen lock. A concurrent `task_close` can stop the process and end the wait.

```json
{"task_id":"<task id>","resource_id":"<process resource id>","exit_code":0,"timeout_ms":10000}
```

Omit `exit_code` to accept any observed exit code. The default deadline is 10 seconds. The maximum is 10 minutes. Inspection and polling share one deadline. Request cancellation reaches lock acquisition and systemd queries.

The result contains `status`, `elapsed_ms`, and `last_observation`:

- `satisfied`: the process exited and its code matches the expectation.
- `failed`: the exit code differs, or the task or resource was closed.
- `timed_out`: no terminal outcome was observed before the deadline.
- `cancelled`: the request was cancelled. A disconnected client may receive a transport cancellation instead of the result.
- `unsupported`: process state or exit evidence is unavailable.

The last observation includes task state and the resource record when available. A missing service without a retained exit code does not prove a successful exit. Invalid task ownership, an unknown resource ID, or an invalid timeout returns a tool error.

Existing `wait` and `find` inputs remain unchanged. Browser integration is not changed by this increment.

## Screen conditions

`condition_wait` accepts an agent `screen`, `success` predicates, optional `failure` predicates, and `timeout_ms`. Any success predicate can satisfy the wait. Any failure predicate takes precedence. A source error returns unsupported. Screen closure or replacement returns failed.

Element predicates use `kind: "element"`, a regular expression in `text`, an optional `role`, and optional `absent: true`. State predicates read the current page. A successful source read is required. OCR cannot prove absence. The source is pinned after the first successful read.

Linux process predicates use `kind: "process"`, `task_id`, `resource_id`, and an optional `exit_code`. Put a process-exit predicate in `failure` to stop a screen wait when its server exits. Unknown ownership or unavailable process evidence returns unsupported.

Request predicates use `kind: "request"`, a URL regular expression in `text`, optional HTTP `status`, and optional `failed: true` for transport errors or HTTP errors. Only completed requests that started after the cursor match. An HTTP response alone does not mean the request body completed. No headers or bodies are collected.

Use `condition_cursor` before a separate action, then pass the returned cursor as `after`. Alternatively, pass an `action` object with the same fields as `act`. The collector is armed before input. The result includes the action ID. A provided cursor and an embedded action cannot be combined. Cursor expiry, lost in-flight requests, and collector changes return unsupported with an evidence-gap reason.

```json
{
  "screen": "<returned screen name>",
  "success": [{"kind":"element","text":"^Saved$"}],
  "failure": [{"kind":"request","text":"/submit","failed":true}],
  "action": {"ref":"<snapshot ref>","op":"click"},
  "timeout_ms": 10000
}
```

Polling releases the UI lock between observations. Each element observation has a two-second source deadline within the overall deadline. Embedded actions retain the existing input serialization and guard checks. Request evidence redacts URLs and diagnostic text before return. Event order is evidence of timing, not proof of causality.

## Validation

Controlled browser tests cover presence, absence, timeout, failure precedence, input followed by an immediate request, explicit request cursors, collector mismatch, unavailable request sources, and cancellation. Collector tests cover completion, request loss, and expired cursors. Existing process-wait checks remain in place. This does not establish bare engine parity or a live cage UI journey for the new tools.

## Required Bare browser

The default browser is Bare. Use `browser_act` with its native `expect` input for Bare action postconditions. `condition_wait` and `condition_cursor` use native-app accessibility and DevTools sources. They do not fall back to Chrome for a Bare page. A Bare session without the required structured source returns unsupported. Its current text network reports do not provide the stable request-start cursor required by this API.
