# hyprcage perception Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use subagent-driven-development (recommended) or executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `snapshot`, `act` and `find` tools so that an agent drives the applications on an agent screen through text refs instead of screenshots.

**Architecture:** A new package `internal/perceive` holds a shared node model, a ref table and three sources: CDP (Chromium and Electron), AT-SPI (GTK and Qt) and OCR (anything). The MCP server keeps one ref table per screen and calls the source that `Choose` picks. Input reuses the existing virtual pointer and keyboard in `internal/screen/input.go`.

**Tech Stack:** Go 1.27, `github.com/modelcontextprotocol/go-sdk`, `github.com/coder/websocket` (CDP), `github.com/godbus/dbus/v5` (AT-SPI), `tesseract` 5 with `eng` data (OCR).

**Spec:** `docs/superpowers/specs/2026-09-29-hyprcage-perception-design.md`

## Global Constraints

- Default `snapshot` mode is `interactive`. The default `max_nodes` is 300.
- The first line of every snapshot is `source=<cdp|atspi|ocr> nodes=<n> truncated=<true|false>`.
- The node line format is `[<ref>] <role> "<name>"[ value="<v>"] (<x>,<y>)[ <states...>][ level=<n>][ offscreen]`.
- CDP refs are `e<n>`. AT-SPI refs are also `e<n>`. OCR refs are `o<n>`.
- After the input, `act` waits until the tree has not changed for 1 s before the first change, or for 300 ms after a change. The total wait is at most 3 s.
- The DevTools port binds to `127.0.0.1` only.
- `app_launch` always sets `QT_LINUX_ACCESSIBILITY_ALWAYS_ON=1`, `ACCESSIBILITY_ENABLED=1` and `GNOME_ACCESSIBILITY=1` (verified in task 1).
- Perception error codes: `stale_ref`, `no_source`, `ref_offscreen`, `ref_occluded`, `cdp_unreachable`, `unsupported_input`. `unsupported_input` already existed; the other five are new.
- Existing tools keep their behavior and their tests keep passing.
- The AT-SPI source never returns a node from an application outside the screen's process list.
- Commits use the repo-local identity `Arjun KR <arjunkrishnaraj123@gmail.com>` and carry no AI attribution.

## Review Focus

1. **The human's own GTK/Qt/Firefox windows on the shared AT-SPI bus.** A snapshot of an agent screen must never list them. Task 6 adds `TestATSPIFiltersForeignPIDs`.
2. **An Electron application that disables `--remote-debugging-port` through a fuse.** `app_launch` with `debug: true` must return `cdp_unreachable` within 10 s, not hang. `snapshot` with `source: auto` must fall back to AT-SPI or OCR. Task 4 adds `TestLaunchDebugUnreachable` and task 8 adds `TestChooseFallsBackWhenCDPDown`.
3. **Duplicate names, for example five "Close" buttons.** Re-resolution of a stale ref must fail with `stale_ref`, never click one of them at random. Task 3 adds `TestReResolveAmbiguous`.
4. **Large trees (VS Code in `full` mode has 700+ nodes).** `max_nodes` must cap the output in document order and set `truncated=true`. Task 2 adds `TestRenderTruncates`.
5. **Names with quotes, newlines or very long text.** One node must render as one line. Task 2 adds `TestRenderEscapes`.

---

### Task 1: Verify the spec's hypotheses and record fixtures

This task produces facts and fixtures, not product code. Probe scripts stay in `/tmp` and are deleted at the end.

**Files:**

- Modify: `docs/superpowers/specs/2026-09-29-hyprcage-perception-design.md` (the "To verify" section becomes "Verified in task 1")
- Create: `internal/perceive/testdata/cdp_vscode_axtree.json` (raw `Accessibility.getFullAXTree` result from VS Code)
- Create: `internal/perceive/testdata/cdp_vscode_boxes.json` (map of `backendDOMNodeId` to `DOM.getBoxModel` result, for the interactive nodes)
- Create: `internal/perceive/testdata/atspi_thunar.json` (a walk of Thunar's tree: bus name, path, role name, name, states, extents, children)

**Interfaces:**

- Produces: the three fixtures, which later tasks load in their tests. The fixture `bus` values are placeholders (`:fixture.<pid>`), because the Python probe cannot read bus names. The JSON shape of `atspi_thunar.json` is `[{"bus":"...","path":"...","role":"push button","name":"...","states":["focused"],"extents":[x,y,w,h],"parent":"<path or empty>","pid":123}]`. This matches `fakeAccessible` in task 6.

- [ ] **Step 1: Install the OCR language data.** Run `sudo -n pacman -S --needed --noconfirm tesseract-data-eng`. Expected: `tesseract --list-langs` lists `eng`.
- [ ] **Step 2: Check the shared AT-SPI bus and the PID filter.** Create a hyprcage screen. Launch `thunar` on it. On the session bus, list the applications under `org.a11y.atspi.Registry` with `busctl --user --address=$(busctl --user call org.a11y.Bus /org/a11y/bus org.a11y.Bus GetAddress | cut -d'"' -f2)` or with a short Python `gi.repository.Atspi` script. Record whether Thunar appears next to the human's applications and whether its PID is in the processes that carry `HYPRCAGE_SCREEN=<name>`.
- [ ] **Step 3: Check the coordinates.** For one button each in Thunar (GTK3), pavucontrol (GTK4) and qBittorrent (Qt6, launched with `QT_LINUX_ACCESSIBILITY_ALWAYS_ON=1`), compare `GetExtents(WINDOW)` centres with a screenshot at scale 0.5. Record whether they match within 2 px. Record whether GTK4 returns extents at all, and whether Qt publishes a tree without the variable.
- [ ] **Step 4: Check the Electron applications.** For each of `code`, `cursor`, `antigravity`, `figma-linux`, `postman` and `balena-etcher`, launch with `--user-data-dir=/tmp/hc-<app> --remote-debugging-port=<free port>` and query `/json/version`. Record which answer.
- [ ] **Step 5: Check Firefox and Zed.** Record the node count Firefox publishes on AT-SPI, and whether Zed publishes any.
- [ ] **Step 6: Record the fixtures.** Save the three testdata files listed above from the VS Code and Thunar runs.
- [ ] **Step 7: Update the spec.** Replace "To verify" with "Verified in task 1", one line per hypothesis with its result. When a hypothesis fails, write the fallback that the later tasks now use, and flag the change to the human before the next task starts.
- [ ] **Step 8: Destroy the screens and delete `/tmp/hc-*`.**
- [ ] **Step 9: Commit.**

```bash
git add docs/superpowers/specs/2026-09-29-hyprcage-perception-design.md internal/perceive/testdata
git commit -m "perceive: record the verified facts and the source fixtures"
```

---

### Task 2: Node model, rendering and diff

**Files:**

- Create: `internal/perceive/node.go`
- Test: `internal/perceive/node_test.go`

**Interfaces:**

- Produces:

```go
package perceive

type Mode string
const (
	ModeInteractive Mode = "interactive"
	ModeFull        Mode = "full"
)

// Node is one element, whatever the source.
type Node struct {
	Key       string   // source identity: "cdp:<target>:<backendId>", "atspi:<bus>:<path>", "ocr:<n>"
	Parent    string   // Key of the parent, "" at the root
	Ref       string   // set by Table.Assign
	Role      string   // ARIA-style role name
	Name      string
	Value     string
	Desc      string
	Level     int
	States    []string // subset of: focused checked expanded selected disabled required invalid readonly
	X, Y      int      // centre, screen pixels
	Offscreen bool
}

type Header struct {
	Source    string
	Nodes     int
	Truncated bool
}

func Interactive(role string) bool        // controls, landmarks and headings
func Filter(nodes []Node, m Mode, rootKey string) []Node
func Render(h Header, nodes []Node) string
type Change struct{ Before, After Node }
type Diff struct {
	Added, Removed []Node
	Changed        []Change
}
func DiffNodes(before, after []Node) Diff   // matched by Key
func (d Diff) Empty() bool
func (d Diff) String() string               // "+ " added, "- " removed, "~ " changed, each in Render's line format
```

`Interactive` returns true for: button, link, textbox, searchbox, checkbox, radio, combobox, listbox, option, menuitem, menuitemcheckbox, menuitemradio, tab, treeitem, switch, slider, spinbutton, heading, banner, navigation, main, complementary, contentinfo, dialog, alertdialog, search, form, region.

`Filter` with a `rootKey` keeps that node and its descendants (by `Parent`) before it applies the mode.

- [ ] **Step 1: Write the failing tests** in `node_test.go`:
  - `TestRenderLine`: `Node{Ref:"e12", Role:"button", Name:"Save", X:640, Y:388, States:[]string{"focused"}}` renders as `[e12] button "Save" (640,388) focused`.
  - `TestRenderHeader`: the first line is `source=cdp nodes=2 truncated=false`.
  - `TestRenderEscapes`: a name `a "b"\nc` renders as `"a \"b\" c"` on one line. A name longer than 120 runes is cut to 120 runes plus `…`.
  - `TestRenderTruncates`: `Header{Truncated:true}` renders `truncated=true`. (The cap itself is applied in `Snapshot`, task 8. This test pins the header.)
  - `TestFilterInteractive`: from a list with a `generic` node and a `button` node, `ModeInteractive` keeps only the button, and `ModeFull` keeps both.
  - `TestFilterRoot`: with a three-level tree, `rootKey` of the middle node keeps it and its child only.
  - `TestDiff`: before `{a button "Save"}`, after `{a button "Saved"}` and `{b link "Home"}`. The diff has one change (a) and one addition (b). `String()` contains `~ [` and `+ [`.
- [ ] **Step 2: Run `go test ./internal/perceive/ -run 'Render|Filter|Diff'`.** Expected: FAIL (package does not compile).
- [ ] **Step 3: Implement `node.go`** with the interface above.
- [ ] **Step 4: Run the tests again.** Expected: PASS.
- [ ] **Step 5: Commit** with message `perceive: add the node model, rendering and diff`.

---

### Task 3: Ref table and re-resolution

**Files:**

- Create: `internal/perceive/refs.go`
- Test: `internal/perceive/refs_test.go`

**Interfaces:**

- Consumes: `Node` from task 2.
- Produces:

```go
type Table struct{ /* mutex, counters, key→ref, ref→Node */ }
func NewTable() *Table
// Assign sets Ref on each node in place. A known Key keeps its ref.
// A new Key gets the next e<n>, or o<n> when the Key starts with "ocr:".
// Assign replaces the table's last snapshot with nodes.
func (t *Table) Assign(nodes []Node)
func (t *Table) Lookup(ref string) (Node, bool)       // from the last snapshot
// ReResolve looks in fresh for exactly one node with old.Role and old.Name.
func ReResolve(old Node, fresh []Node) (Node, error)  // error is *screen.Error with CodeStaleRef
```

- Modify: `internal/screen/errors.go` — add `CodeStaleRef = "stale_ref"`, `CodeNoSource = "no_source"`, `CodeRefOffscreen = "ref_offscreen"`, `CodeCDP = "cdp_unreachable"`.

- [ ] **Step 1: Write the failing tests:**
  - `TestAssignStable`: assign `[k1,k2]`, then `[k2,k3]`. k2 keeps `e2`, and k3 gets `e3`.
  - `TestAssignOCRPrefix`: a Key `ocr:0` gets `o1`.
  - `TestLookup`: after assign, `Lookup("e1")` returns the node. `Lookup("e99")` returns false.
  - `TestReResolveUnique`: one fresh node with the same role and name is returned.
  - `TestReResolveAmbiguous`: two fresh "button Close" nodes give an error whose `Code` is `stale_ref`.
  - `TestReResolveMissing`: no match gives `stale_ref`.
- [ ] **Step 2: Run `go test ./internal/perceive/ -run 'Assign|Lookup|ReResolve'`.** Expected: FAIL.
- [ ] **Step 3: Implement `refs.go` and the error codes.**
- [ ] **Step 4: Run the tests again.** Expected: PASS.
- [ ] **Step 5: Commit** with message `perceive: add the ref table and re-resolution`.

---

### Task 4: `app_launch` debug mode and the screen's DevTools port

**Files:**

- Modify: `internal/registry/registry.go` — add `DebugPort int \`json:"debug_port,omitempty"\``to`Screen`.
- Create: `internal/screen/debug.go`
- Modify: `internal/screen/launch.go` — always set `QT_LINUX_ACCESSIBILITY_ALWAYS_ON=1`, `ACCESSIBILITY_ENABLED=1` and `GNOME_ACCESSIBILITY=1` in `env`, before `extraEnv` so a caller can override them.
- Modify: `internal/mcpserver/server.go` — `launchIn` gains `Debug bool \`json:"debug,omitempty" jsonschema:"Chromium or Electron app: open the DevTools port that snapshot, act and find read"\``; `appLaunch`calls`screen.DebugArgs`and`screen.WaitDebug` when it is true.
- Modify: `internal/browser/browser.go` — `Open` sets `rec.DebugPort = port` and saves the record.
- Modify: `internal/cli/launch.go` — a `-debug` flag with the same effect.
- Test: `internal/screen/debug_test.go`

**Interfaces:**

- Produces:

```go
// FreePort returns a free TCP port on 127.0.0.1.
func FreePort() (int, error)
// DebugArgs appends the DevTools flags to command.
func DebugArgs(command []string, port int) []string
// WaitDebug polls http://127.0.0.1:<port>/json/version until it answers.
// It returns *Error with CodeCDP after timeout.
func WaitDebug(port int, timeout time.Duration) error
```

`DebugArgs` appends `--remote-debugging-port=<port>`, `--remote-debugging-address=127.0.0.1` and `--force-renderer-accessibility`. `appLaunch` uses a 10 s timeout. It refuses with `CodeLimit` when `rec.DebugPort` is already set, because a screen runs one application.

- [ ] **Step 1: Write the failing tests:**
  - `TestDebugArgs`: `DebugArgs([]string{"code"}, 9333)` equals `{"code","--remote-debugging-port=9333","--remote-debugging-address=127.0.0.1","--force-renderer-accessibility"}`.
  - `TestFreePort`: the port is in 1024–65535 and `net.Listen` on it succeeds.
  - `TestLaunchDebugUnreachable`: `WaitDebug` on a free port with a 300 ms timeout returns an error with `Code == CodeCDP` in under 1 s.
  - `TestWaitDebugReady`: an `httptest` server that answers `/json/version` makes `WaitDebug` return nil. (Parse the port from the test server URL.)
- [ ] **Step 2: Run `go test ./internal/screen/ -run Debug`.** Expected: FAIL.
- [ ] **Step 3: Implement `debug.go`, then wire the registry field, `launch.go`, `appLaunch`, `browser.Open` and the CLI flag.**
- [ ] **Step 4: Run `go test ./...`.** Expected: PASS, including the existing server tests.
- [ ] **Step 5: Commit** with message `launch: open a DevTools port per screen on request`.

---

### Task 5: CDP source

**Files:**

- Create: `internal/perceive/source.go` (the `Source` interface only)
- Create: `internal/perceive/cdp.go`
- Test: `internal/perceive/cdp_test.go`
- Modify: `go.mod`, `go.sum` — add `github.com/coder/websocket`.

**Interfaces:**

- Consumes: `Node`, `Mode` (task 2). The `rec.DebugPort` from task 4. The fixtures from task 1.
- Produces:

```go
type Source interface {
	Name() string // "cdp", "atspi" or "ocr"
	// Nodes returns every node in document order, unfiltered.
	Nodes(ctx context.Context) ([]Node, error)
	// Reveal scrolls the node into view and returns it with fresh coordinates.
	Reveal(ctx context.Context, key string) (Node, error)
	// Press activates the node without the pointer. Only AT-SPI implements it.
	Press(ctx context.Context, key string) error
	Close() error
}

// caller sends one CDP command on one session and returns its result.
type caller interface {
	Call(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error)
}

func NewCDP(ctx context.Context, port int) (Source, error)   // dials the browser websocket from /json/version
func newCDPWith(c caller, targets []string) Source           // for tests
```

Behavior:

1. `NewCDP` reads `webSocketDebuggerUrl` from `/json/version` and dials it.
2. `Nodes` calls `Target.getTargets` and `Target.attachToTarget` with `flatten: true` for each `page`, `iframe` and `webview` target, once per target, and remembers the session.
3. For each session, `Nodes` calls `Accessibility.getFullAXTree`, drops ignored nodes, and maps the AX properties to `States` and `Level`.
4. For each node with a `backendDOMNodeId`, `Nodes` calls `DOM.getBoxModel`. The centre is the mean of the `border` quad, multiplied by `window.devicePixelRatio` read once per session through `Runtime.evaluate`.
5. A node whose centre is outside the viewport (`innerWidth` × `innerHeight`) gets `Offscreen = true`. A node without a box keeps (0,0) and `Offscreen = true`.
6. `Reveal` calls `DOM.scrollIntoViewIfNeeded`, then `DOM.getBoxModel` again.
7. `Press` returns an error: CDP clicks use the pointer.
8. A dial or call failure returns `*screen.Error` with `CodeCDP`.

- [ ] **Step 1: Write the failing tests.** Use a `fakeCaller` that answers from the task 1 fixtures, keyed by method (and by `backendNodeId` for `DOM.getBoxModel`).
  - `TestCDPNodesFromFixture`: the result contains a `button` named `Continue without Signing In` at (991,650) ± 2.
  - `TestCDPIgnoredDropped`: no node with `ignored: true` in the fixture is returned.
  - `TestCDPStates`: a fixture node with `focused: true` has `"focused"` in `States`.
  - `TestCDPKeys`: every Key starts with `cdp:` and is unique.
  - `TestCDPUnreachable`: `NewCDP` on a free port returns an error with `Code == CodeCDP`.
- [ ] **Step 2: Run `go test ./internal/perceive/ -run CDP`.** Expected: FAIL.
- [ ] **Step 3: Implement `source.go` and `cdp.go`.**
- [ ] **Step 4: Run the tests again.** Expected: PASS.
- [ ] **Step 5: Commit** with message `perceive: read Chromium and Electron trees over CDP`.

---

### Task 6: AT-SPI source

**Files:**

- Create: `internal/perceive/atspi.go`
- Test: `internal/perceive/atspi_test.go`
- Modify: `go.mod`, `go.sum` — add `github.com/godbus/dbus/v5`.
- Modify: `internal/screen/procs.go` — export `ScreenProcesses(name string) []int` (rename `screenProcesses`, and update its callers).

**Interfaces:**

- Consumes: `Source` (task 5), `Node` (task 2), `ScreenProcesses`, the `atspi_thunar.json` fixture.
- Produces:

```go
// accessible is one AT-SPI object, as the walk needs it.
type accessible struct {
	Bus, Path, Parent string
	Role, Name        string
	States            []string
	Extents           [4]int // x, y, w, h in window coordinates; w == 0 when absent
	PID               int
}

// tree lists every object of the applications on the bus.
type tree interface {
	Walk(ctx context.Context) ([]accessible, error)
	Scroll(ctx context.Context, bus, path string) error      // Component.ScrollTo
	DoAction(ctx context.Context, bus, path string, i int) error
}

func NewATSPI(ctx context.Context, screenName string) (Source, error)
func newATSPIWith(t tree, pids []int) Source   // for tests
// HasApps reports whether any application on the bus belongs to the screen.
func HasApps(ctx context.Context, screenName string) bool
```

Behavior:

1. `NewATSPI` gets the bus address from `org.a11y.Bus.GetAddress` on the session bus and connects to it.
2. `Walk` lists the children of `/org/a11y/atspi/accessible/root` on `org.a11y.atspi.Registry`. It skips every application whose PID (from `org.freedesktop.DBus.GetConnectionUnixProcessID`) is not in `ScreenProcesses(screenName)`. It walks the kept applications depth first, with a cap of 5 000 objects.
3. A role maps to the ARIA-style name through a table in `atspi.go`: `button`→`button` (at-spi2-core 2.60 name), `push button`→`button`, `toggle button`→`button`, `check box`→`checkbox`, `radio button`→`radio`, `text`/`entry`/`password text`→`textbox`, `combo box`→`combobox`, `list item`→`option`, `menu item`→`menuitem`, `page tab`→`tab`, `tree item`/`table cell` inside a tree→`treeitem`, `heading`→`heading`, `dialog`→`dialog`, `link`→`link`, `slider`→`slider`, `spin button`→`spinbutton`. Other roles keep their AT-SPI name with spaces replaced by `_`.
4. Extents use the window coordinate type (GTK4 returns (0,0) for the screen type). The centre is (x + w/2, y + h/2). An object with `w == 0`, or with x or y equal to -2147483648 (GTK3 hidden widgets), gets `Offscreen = true`.
5. `Reveal` calls `Scroll`, then re-walks the one object.
6. `Press` calls `DoAction` with index 0.

Use the result of task 1 for the coordinate type. If task 1 found that window coordinates do not equal screen pixels, follow the fallback the updated spec names.

- [ ] **Step 1: Write the failing tests** with a `fakeTree` loaded from `atspi_thunar.json`:
  - `TestATSPIFiltersForeignPIDs`: add an object with PID 999999 to the fake. `Nodes` with `pids = []int{<thunar pid from fixture>}` never returns it.
  - `TestATSPIRoleMap`: a `push button` object and a `button` object both become nodes with role `button`.
  - `TestATSPICentre`: extents (100,200,40,20) give (120,210).
  - `TestATSPINoExtents`: `w == 0` gives `Offscreen == true`, and so do extents (-2147483648,-2147483648,1,1).
  - `TestATSPIFixtureButtons`: the Thunar fixture yields a `button` named `Back` at (22,48) and one named `Home` at (133,48).
  - `TestATSPIPress`: `Press` calls `DoAction` on the object's bus and path with index 0.
- [ ] **Step 2: Run `go test ./internal/perceive/ -run ATSPI`.** Expected: FAIL.
- [ ] **Step 3: Implement `atspi.go` and export `ScreenProcesses`.**
- [ ] **Step 4: Run `go test ./...`.** Expected: PASS.
- [ ] **Step 5: Commit** with message `perceive: read GTK and Qt trees over AT-SPI, filtered by screen`.

---

### Task 7: OCR source and the language data package

**Files:**

- Create: `internal/perceive/ocr.go`
- Test: `internal/perceive/ocr_test.go`, `internal/perceive/testdata/ocr_lines.tsv` (a TSV output of `tesseract` on the task 1 VS Code screenshot)
- Modify: `internal/setup/setup.go` — add a `File` field to the `Packages` entries. An entry with `File` counts as present when that file exists. Add `{Package: "tesseract", Binary: "tesseract"}` and `{Package: "tesseract-data-eng", File: "/usr/share/tessdata/eng.traineddata"}`. On zypper the package names are `tesseract-ocr` and `tesseract-ocr-traineddata-english`. Map them in `InstallArgv`.
- Modify: `install.sh` — the same two packages in `PACKAGES`, with the zypper names.

**Interfaces:**

- Consumes: `Source`, `Node`, `screen.Shot`.
- Produces:

```go
// OCRData is the path of the English language data.
const OCRData = "/usr/share/tessdata/eng.traineddata"
func NewOCR(cl *wl.Client) Source
// parseTSV turns tesseract TSV into one node per line, in reading order.
func parseTSV(tsv []byte) []Node
```

Behavior:

1. `Nodes` captures the screen with `screen.Shot` at scale 1 in PNG format, pipes it into `tesseract - - -l eng tsv`, and calls `parseTSV`.
2. `parseTSV` groups the words that share (`block_num`, `par_num`, `line_num`) and have a confidence of at least 60. It joins them with spaces. The box is the union of the word boxes. The Key is `ocr:<index>`. The role is `text`.
3. `Reveal` returns the node unchanged. `Press` returns an error.

- [ ] **Step 1: Write the failing tests:**
  - `TestParseTSVLines`: the fixture gives a node named `Continue without Signing In` with a centre within 10 px of (990,650).
  - `TestParseTSVLowConfidence`: a word with confidence 30 is not in any name.
  - `TestParseTSVKeys`: Keys are `ocr:0`, `ocr:1`, … in order.
  - `TestMissingFilePackage` (in `internal/setup`): an entry with `File` set to a path that does not exist is listed by `Missing()`.
- [ ] **Step 2: Run `go test ./internal/perceive/ ./internal/setup/ -run 'TSV|Missing'`.** Expected: FAIL.
- [ ] **Step 3: Implement `ocr.go` and the setup and installer changes.**
- [ ] **Step 4: Run the tests again.** Expected: PASS.
- [ ] **Step 5: Commit** with message `perceive: add the OCR source and install its language data`.

---

### Task 8: Source choice, `Snapshot`, `Act` and `Find`

**Files:**

- Modify: `internal/perceive/source.go`
- Create: `internal/perceive/act.go`
- Test: `internal/perceive/act_test.go`

**Interfaces:**

- Consumes: everything from tasks 2–7; `screen.Click`, `screen.ScrollAt`, `screen.Keys`, `wl.Client.Type` (the method the existing `type` tool uses) and `wl.Client.Move`.
- Produces:

```go
// Choose picks the source. want is "auto", "cdp", "atspi" or "ocr".
// auto tries CDP when rec.DebugPort > 0 and it answers, then AT-SPI when
// HasApps, then OCR when OCRData exists. It returns CodeNoSource otherwise.
func Choose(ctx context.Context, rec *registry.Screen, cl *wl.Client, want string) (Source, error)

type SnapOpts struct {
	Mode     Mode
	RootRef  string
	MaxNodes int // 0 means 300
}
// Snapshot reads, filters, caps to MaxNodes in document order, assigns refs
// and renders.
func Snapshot(ctx context.Context, src Source, t *Table, o SnapOpts) (string, []Node, error)

type ActOp struct {
	Ref  string
	Op   string   // click, double_click, type, key, hover, scroll
	Text string
	Keys []string
	Direction string // scroll only, default "down"
}
// Act resolves the ref, reveals it when off screen, sends the input,
// waits for the tree to settle, and returns the diff.
func Act(ctx context.Context, cl *wl.Client, src Source, t *Table, op ActOp) (Diff, error)

// Find returns the nodes whose name or value matches re (and role, when set).
// With timeout > 0 it polls every 250 ms until a match appears.
func Find(ctx context.Context, src Source, t *Table, re *regexp.Regexp, role string, timeout time.Duration) ([]Node, error)
```

`Act` details:

1. `Lookup` the ref. When it is missing from the current nodes, call `ReResolve` against a fresh `Nodes` read.
2. When the node is off screen, call `Reveal`. When it is still off screen and the source is AT-SPI and the op is `click`, call `Press` instead of the pointer. Otherwise return `CodeRefOffscreen`.
3. For `type`, click the node first, then type the text.
4. Settle: read `Nodes` every 100 ms. Stop when `DiffNodes` has been empty for 300 ms, or after 3 s.
5. The diff compares the filtered interactive nodes before and after. The new nodes go through `Table.Assign`.

- [ ] **Step 1: Write the failing tests** with a `fakeSource` (a list of node lists returned in turn) and a nil `wl.Client` replaced by an `inputter` interface that `Act` takes internally for tests (`act(ctx, in inputter, …)`; the exported `Act` wraps the `wl.Client`):
  - `TestSnapshotCapsAndFlags`: 400 interactive nodes with `MaxNodes` 300 give 300 lines plus the header, and `truncated=true`.
  - `TestSnapshotRoot`: `RootRef` limits the output to a subtree.
  - `TestActClickSendsCentre`: the fake inputter records a click at the node centre.
  - `TestActReturnsDiff`: the second read renames a button. The diff has one change.
  - `TestActSettleTimeout`: a source that changes on every read makes `Act` return after about 3 s with a diff, not an error.
  - `TestActOffscreenPressFallback`: an AT-SPI-named fake source with an off-screen node makes `Act` call `Press`, not the inputter.
  - `TestFindWaits`: the match appears on the third read. `Find` with a 2 s timeout returns it.
  - `TestChooseFallsBackWhenCDPDown`: a record with `DebugPort` set to a free port and a present OCR file (injected through a package variable `ocrData`) makes `Choose("auto")` return the OCR source.
  - `TestChooseNoSource`: no port, no AT-SPI apps (injected `hasApps` returns false) and no OCR file give `CodeNoSource`.
- [ ] **Step 2: Run `go test ./internal/perceive/ -run 'Snapshot|Act|Find|Choose'`.** Expected: FAIL.
- [ ] **Step 3: Implement `Choose` in `source.go`, then `act.go`.**
- [ ] **Step 4: Run the tests again.** Expected: PASS.
- [ ] **Step 5: Commit** with message `perceive: add snapshot, act and find over any source`.

---

### Task 9: MCP tools, CLI twins, doctor and docs

**Files:**

- Create: `internal/mcpserver/server_perceive.go`
- Modify: `internal/mcpserver/server.go` — `Server` gains `tables map[string]*perceive.Table` with a mutex. `register` calls `s.registerPerceive(srv)`. `screenDestroy` drops the screen's table.
- Create: `internal/cli/perceive.go` — `hyprcage snapshot [screen]`, `hyprcage act [screen] <ref> <op> [text]`, `hyprcage find [screen] <regexp>`. Register them in the command table in `cli.go` next to `clip`.
- Modify: `internal/cli/doctor.go` — check the AT-SPI bus (`org.a11y.Bus` answers `GetAddress`) and `perceive.OCRData`.
- Modify: `skills/hyprcage/SKILL.md` and `README.md` — document the three tools and `debug`. The skill tells the agent to call `snapshot` first, to act by ref, and to take a screenshot only when the snapshot does not explain the screen.
- Test: `internal/mcpserver/server_test.go`

**Interfaces:**

- Consumes: `Choose`, `Snapshot`, `Act`, `Find`, `NewTable`.
- Produces: the tools `snapshot`, `act` and `find` with these inputs:

```go
type snapIn struct {
	Screen   string `json:"screen,omitempty"`
	Mode     string `json:"mode,omitempty" jsonschema:"interactive (default: controls, landmarks, headings) or full"`
	Root     string `json:"root,omitempty" jsonschema:"ref of a subtree to read instead of the whole screen"`
	MaxNodes int    `json:"max_nodes,omitempty" jsonschema:"cap on the elements returned, default 300"`
	Source   string `json:"source,omitempty" jsonschema:"auto (default), cdp, atspi or ocr"`
}
type actIn struct {
	Screen          string   `json:"screen,omitempty"`
	Ref             string   `json:"ref"`
	Op              string   `json:"op" jsonschema:"click, double_click, type, key, hover or scroll"`
	Text            string   `json:"text,omitempty"`
	Keys            []string `json:"keys,omitempty"`
	Direction       string   `json:"direction,omitempty"`
	ScreenshotAfter bool     `json:"screenshot_after,omitempty"`
}
type findIn struct {
	Screen    string `json:"screen,omitempty"`
	Text      string `json:"text" jsonschema:"regular expression on the element name or value"`
	Role      string `json:"role,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty" jsonschema:"wait up to this long for a match, default 0"`
}
```

Tool descriptions:

- `snapshot`: "List the elements of a screen as text with refs and screen coordinates. Read this before a screenshot: it costs a fraction of the tokens."
- `act`: "Click, type, press keys, hover or scroll on an element by its ref from snapshot. Returns what changed on the screen as text."
- `find`: "Find elements by name or value, optionally waiting until one appears. Use it instead of wait plus screenshot."

`act` returns the diff as text. With `screenshot_after` it also returns the image through the existing `afterAction`.

- [ ] **Step 1: Write the failing tests** in `server_test.go`:
  - `TestPerceiveToolsListed`: `ListTools` contains `snapshot`, `act` and `find`.
  - `TestSnapshotUnknownScreen`: `snapshot` with `screen: "nope"` returns an error result that contains `screen_not_found`.
  - `TestActMissingRef`: `act` without `ref` returns an error result.
- [ ] **Step 2: Run `go test ./internal/mcpserver/`.** Expected: FAIL.
- [ ] **Step 3: Implement `server_perceive.go`, the table field, the CLI twins, the doctor checks and the docs.**
- [ ] **Step 4: Run `go test ./...` and `go vet ./...`.** Expected: PASS and no output.
- [ ] **Step 5: Commit** with message `mcp: expose snapshot, act and find`.

---

### Task 10: Smoke script and E2E

**Files:**

- Create: `tests/smoke-perceive.sh`

**Interfaces:**

- Consumes: the CLI twins from task 9.

The script runs these steps for each application. It reports every failure by step, the same way the existing fork smoke test does.

| App       | Launch                                                                              | `act` target                                                        | Expected diff                            |
| --------- | ----------------------------------------------------------------------------------- | ------------------------------------------------------------------- | ---------------------------------------- |
| VS Code   | `code --user-data-dir=/tmp/hc-code --extensions-dir=/tmp/hc-code-ext` with `-debug` | the ref named `Continue without Signing In`                         | the dialog's buttons are in `- ` removed |
| Chrome    | `browser_open` equivalent, `hyprcage browser <screen> https://example.org`          | the link `More information...` (or its current name on example.org) | a new heading appears                    |
| Thunar    | `thunar /tmp`                                                                       | the menu item `View`                                                | new `menuitem` lines in `+ ` added       |
| Wireshark | `wireshark`                                                                         | the menu item `Help`                                                | new `menuitem` lines in `+ ` added       |

- [ ] **Step 1: Build and install the binary.** Run `make build && install -Dm755 hyprcage ~/.local/bin/hyprcage`.
- [ ] **Step 2: Write `tests/smoke-perceive.sh`.** For each row: `create`, `launch`, `snapshot`, `find` the target, `act click`, check the diff with `grep`, `destroy`. It exits non-zero when any step fails.
- [ ] **Step 3: Run it.** Expected: every row passes.
- [ ] **Step 4: Run the E2E in a hyprcage screen with the mirror open.** Register the fork's MCP server for the session (`install.sh` from the checkout does this). For each app, do one scripted task through the MCP tools only: in VS Code open a file through the Explorer, in Chrome follow a link, in Thunar open a folder, and in Wireshark open the About dialog. Count the screenshots for each task. Expected: at most one per task.
- [ ] **Step 5: Write the result in `docs/worklog-2026-09-29.md`** (or the worklog for the day of the run): the screenshot count and the token count for each task.
- [ ] **Step 6: Commit** with message `test: add the perception smoke test`.
