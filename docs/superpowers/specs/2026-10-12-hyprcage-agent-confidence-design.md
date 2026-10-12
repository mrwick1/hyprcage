# hyprcage agent confidence: verify, watch, guard and read tools

Date: 2026-10-12
Status: draft, for review
Base branch: `main`

## Goal

Let the agent know whether an action worked, without a screenshot after every action.

1. Each action reports whether the screen changed as intended.
2. The agent can watch a screen for a period and read whether it is stable, jittering or frozen.
3. A destructive action on the human's desktop needs an explicit acknowledgement.
4. The agent can read exact selected text, a labelled tour of UI states, and a whole scrollable page.

The ideas come from a review of `88plug/screen-mcp` (2026-10-12). The review read the README only. Nothing in this spec copies its code.

## Non-goals

- Change how an action runs. Verification only observes.
- Add OCR or icon grounding. Perception (sub-project A) covers the screen reads.
- Add a guard for agent screens. A screen is disposable, and the human does not share it.
- Replace `screenshot_after`. A screenshot stays the way to see the result.

## Facts from the code (origin/main, 2026-10-12)

- `internal/record/record.go` has `Loop` with a frame-diff threshold for idle cuts and a `Capturer` interface with `Capture(bool)`.
- Every action input struct in `internal/mcpserver/server.go` has `screenshot_after`.
- None of the tools in this spec exist today. `git grep` for verify, watch, guard, read_selection, tour and read_page found no tool.
- `internal/screen/capture.go` holds the capture path for agent screens.
- To verify: the capture path for `screen: "desktop"`. The spec assumes it can return an `image.RGBA` for a window.

## Decisions

| Question | Decision | Reason |
| --- | --- | --- |
| Guard default | On for `screen: "desktop"`, off for agent screens. | Desktop windows belong to the human. Agent screens are disposable. |
| Verify default | On, with `verify: false` to opt out. A measurement gate applies (see Part 2). | A verdict on every action is the purpose of the feature. |
| `watch` output | Writes frames to a `path`, like `screenshot`. | The result stays small. The agent opens a frame only when needed. |

## Part 1: frame-diff core

Package `internal/perceive/diff.go`. It has no tool surface.

- Input: two `*image.RGBA` frames.
- Output: `Diff{Ratio float64, BBox image.Rectangle}`. `Ratio` is the fraction of changed pixels.
- Reuse the pixel-compare and threshold code of the recorder. Move the shared function to one place. Do not copy it.
- Ignore the mouse pointer and a one-pixel blink area. A blinking caret must not count as a change.

## Part 2: verify and the sense hint

Every action tool accepts `verify` (default true).

The action captures a frame before it runs and a frame after the settle delay. The result gains one line:

`verify: CONFIRMED | PARTIAL | NO_OP | DIVERGED (changed 4.2%, bbox 120,80 640x300)`

| Verdict | Meaning |
| --- | --- |
| NO_OP | The change ratio is below the noise floor. |
| CONFIRMED | The changed area contains the target of the action. |
| PARTIAL | The screen changed, and part of the target did not. |
| DIVERGED | The screen changed far from the target. |

- The result also gains a `sense` object: `{changed, opened, modal, no_op}`. `opened` is true when the window list gained a window. `modal` is true when a new window has a dialog role. `sense` follows `verify`.
- The verdict is a heuristic. The result states the change ratio so the agent can judge it.
- Animated pages (video, spinners) can cause a false CONFIRMED. The agent uses `watch` for those.
- **Measurement gate:** before the PR merges, measure the added time per action on a 1280x800 screen. If the median exceeds 150 ms, the default becomes `verify: false`.

## Part 3: watch

New tool `watch`: `screen`, `seconds` (default 5, max 30), `fps` (default 1, max 4), `path` (optional directory).

- It samples frames and diffs each frame against the previous one.
- Verdict `stable`: no frame changes after the first.
- Verdict `jitter`: the changed ratio stays above the noise floor for more than half of the frames.
- Verdict `frozen`: the screen never changes, and the caller passed `expect_change: true`.
- The result lists the verdict, the number of frames and the per-frame ratios.
- With `path`, the tool writes each frame as a PNG and lists the file names. Without `path`, it returns three thumbnails: first, middle and last.

## Part 4: ack guard

New config table `[guard]` in `config.toml`:

- `enabled`: `"desktop"` (default), `"all"` or `"off"`.
- `apps`: an allowlist of app ids. Empty means no allowlist.
- `keywords`: destructive words that block a click when they match the label of the target. Defaults: delete, remove, erase, format, uninstall, close all, quit, shut down.
- `combos`: blocked key combinations. Defaults: `Ctrl+W`, `Ctrl+Q`, `Alt+F4`.

A blocked action returns the error code `ack_required` and the reason. The agent repeats the call with `ack: "<reason>"`. The server writes the reason to the audit log.

- An action on an app outside the allowlist is blocked the same way.
- The guard checks the label through the perception layer (AT-SPI first). If no label is available, the guard does not block on keywords.

## Part 5: read tools and audit log

- `read_selection`: copies the selection of the focused window and returns the text. It restores the previous clipboard of that screen.
- `tour`: takes a list of actions. After each action it captures a thumbnail with a label. It returns the thumbnails.
- `read_page`: scrolls a view from top to bottom, takes a snapshot at each step and returns the merged list of nodes without duplicates. It stops when two snapshots are equal.
- Audit log: `~/.local/state/hyprcage/actions.jsonl`, one JSON line per action: time, screen, tool, arguments without text content, verdict, `ack`. Typed text is never logged.

## Delivery

One spec, five pull requests in this order: Part 1 with Part 2, Part 3, Part 4, Part 5. Each PR bumps the plugin version and updates `skills/hyprcage/SKILL.md`.

## Tests

- Unit: frame diff on fixture images (identical, caret blink, moved window, full change). Verdict table for each case.
- Unit: guard decisions for each combo, keyword and allowlist case, with and without `ack`.
- E2E on a nested Hyprland (`tests/nested-hypr.sh`): click a button that changes the screen, click a dead area, run `watch` on a page with a spinner, trigger the guard on a desktop window in the hidden test workspace.
- Browser E2E through `screen-driver` after each PR, as the standing rule requires.

## Open items

- To verify: how long a frame capture takes on an agent screen and on a desktop window. The Part 2 gate depends on it.
- To verify: that the focused-window selection copy works for Chrome and for a GTK application without a visible side effect.
- To decide in review: whether `tour` belongs in this spec or in a later one. It overlaps with `batch`.
