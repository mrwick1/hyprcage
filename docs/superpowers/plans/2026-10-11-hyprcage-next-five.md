# hyprcage: the next five items

Date: 2026-10-11. Status: plan, not started.

This plan adds five items to hyprcage. Each item is one branch and one PR. Do them in the order below, because the early items are small and help the later ones. The `screen-driver` agent (dotfiles, `shared/claude/agents/screen-driver.md`) drives every E2E for these items.

## Order

| # | Item | Size | Depends on |
|---|------|------|------------|
| 1 | Screenshot to a file | S | none |
| 2 | `devtools_network` | M | none |
| 3 | Saved browser profiles | M | none |
| 4 | `devtools_audit` (Lighthouse) | M | none |
| 5 | Sub-project C (OS tools) | spec first | Arjun's scope decision |

## 1. Screenshot to a file

**Problem.** `screenshot` returns only an inline image (`internal/mcpserver/server.go:410`, `imageResult` at `:672`). A subagent that fails a check cannot hand the evidence to its parent without putting the image into its own reply.

**Change.**

- Add an optional `path` parameter to `screenshot`. Write a PNG to that path and return the path as text, with no inline image.
- With `path: "auto"`, write to `devtools.DefaultPath` style: `<UserCacheDir>/hyprcage/shots/<screen>-<ts>.png`. `devtools_trace` and `devtools_heap` use the same pattern (`server_browser.go:209-211`).
- Update `screen-driver`: on `FAIL` or `BLOCKED`, save one screenshot with `path: "auto"` and put the path in the report. The parent reads the file only when it needs to.

**Tests.** A unit test for the path branch in `internal/mcpserver`. A manual run that checks the file is a valid PNG of the right size.

## 2. `devtools_network`

**Problem.** No code reads CDP `Network.*` events. `devtools.go:446-447` sends `Network.enable` only for throttling. A failed API call (4xx, 5xx, CORS, DNS) shows up only when Chrome also logs it to the console.

**Change.**

- In `internal/devtools/devtools.go`, call `Network.enable` on each attached page, next to `Runtime.enable` and `Log.enable` (`:161-162`).
- Handle `Network.requestWillBeSent`, `Network.responseReceived`, `Network.loadingFailed` and `Network.loadingFinished`. Keep one entry per request: method, URL, status, error text, resource type, duration.
- Keep a ring buffer capped like the console buffer (`MaxConsole = 1000`, `:26`), and count the dropped entries.
- Add the tool `devtools_network` with `failed_only` (default `true`), `url_contains` and `clear`. A failed request is status 400 or higher, or a `loadingFailed` event.
- Do not store request or response bodies. Bodies can hold tokens and personal data, and they make the buffer large.
- Update `screen-driver`: report failed requests next to console errors.

**Tests.** Fake-CDP unit tests beside `TestConsoleCollectsMessagesExceptionsAndLogEntries` (`devtools_test.go:69`): one request that succeeds, one with status 500, one `loadingFailed`, and the cap. Add the tool name to the list in `server_test.go:55-57`.

## 3. Saved browser profiles

**Problem.** `browser_open` makes a new temp profile on every call (`internal/browser/browser.go:158`) and `screen.Destroy` deletes it (`screen/destroy.go:46`, `:83`). Every E2E must log in again, including before each recording.

**Change.**

- Add an optional `profile` parameter to `browser_open`, a name such as `tc-staging`. Keep the profile in `~/.local/share/hyprcage/profiles/<name>`, mode 700.
- `screen.Destroy` must not delete a named profile. The temp profile stays the default when `profile` is not given.
- Refuse a second screen on a profile that is in use, with a clear error. Chrome locks its user-data-dir, so two instances on one profile fail. To verify: the error that Chrome gives today.
- Validate the name against `^[a-z0-9-]{1,40}$`, so a name cannot escape the profiles directory.
- Add `hyprcage profile list` and `hyprcage profile rm <name>` to the CLI.
- Update `screen-driver`: use the profile that the parent names.

**Security.** A profile holds live session cookies. Never put a profile in a repo, in a recording directory or in a backup that leaves the machine. The README must say this.

**Tests.** Unit tests for the name check and for "destroy keeps a named profile". A manual run: log in once, destroy the screen, open a new screen on the same profile, confirm the session is still logged in.

## 4. `devtools_audit` (Lighthouse)

**Problem.** Lighthouse is the only reason the Chrome DevTools MCP is still installed. hyprcage has no Lighthouse support today.

**Change.**

- Add the tool `devtools_audit` with `url`, `categories` (default: performance, accessibility, best-practices, SEO), `form_factor` (`mobile` or `desktop`) and an optional `path`.
- Run `npx lighthouse <url> --port <rec.DebugPort> --output json --output-path <path>` against the screen's own Chrome. Write the full report to `<UserCacheDir>/hyprcage/devtools/<screen>-<ts>.json`.
- Return the four scores and the 10 worst audits by weight, each with its title and display value, and the report path.
- When `npx` or Lighthouse is missing, return an error that names the install command. Do not install anything from inside the tool.

**To verify before the build.**

- That `lighthouse --port` works against a hyprcage browser. This is still an open item from 2026-09-30.
- Whether Lighthouse opens a new tab or reuses the current one, and if the screen stays usable afterwards.
- How long one run takes on archMachine and on arjun-e16. Set the tool timeout from that number.

**Tests.** A unit test for the summary step with a fixed Lighthouse JSON in `testdata/`. A manual run on one real page.

## 5. Sub-project C (OS tools)

**Problem.** The specs name C as "OS tools" and nothing more (`docs/superpowers/specs/2026-09-29-hyprcage-perception-design.md:13`, `2026-09-30-hyprcage-desktop-design.md:11`, `:26`). The 2026-09-30 notes say that Bash already covers most of it. No spec exists.

**Next step.** Arjun decides the scope before any code. Candidates that Bash does not do well today:

- File pickers and portal dialogs (open and save) on an agent screen.
- Audio: the screen's own sink, to record audio with a video or to check that an app plays sound.
- System dialogs on the desktop, for example a polkit password prompt, shown to Arjun and never answered by the agent.

When Arjun picks the scope, write `docs/superpowers/specs/<date>-hyprcage-os-tools-design.md` and add the build steps to this plan.

## For every item

1. Branch from `main` in a worktree (`git worktree add -b <branch> ../hyprcage-<slug> origin/main`).
2. Run `go test ./...`.
3. Rebuild with `bash install.sh --binary-only`, then run the E2E through `screen-driver`.
4. Bump `.claude-plugin/plugin.json` when a tool or the skill changes, and update `skills/hyprcage/SKILL.md` and the README.
5. Ask Arjun about a recording, then open the PR.
6. After the merge, rebuild on archMachine and arjun-e16, and run `claude plugin marketplace update hyprcage-mrwick1` on both.
