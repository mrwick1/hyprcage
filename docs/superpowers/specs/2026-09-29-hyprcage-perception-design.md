# hyprcage perception: drive agent screens by text, not screenshots

Date: 2026-09-29
Status: approved; task 1 facts recorded
Base branch: `feat/computer-use`

## Goal

Let an agent drive the applications on an agent screen through text: an element list with refs, actions on those refs, and a text diff after each action. Screenshots become the last resort.

A screenshot costs about 1 300 tokens. Unattended tasks of medium length (not hours) take many steps, so screenshot cost dominates. This work cuts that cost.

This is sub-project A of three. Sub-project B (control of the human's desktop) and sub-project C (OS tools) get their own specs later.

## Scope

In scope:

- Chromium and Electron applications, read through the Chrome DevTools Protocol (CDP).
- GTK and Qt applications, read through AT-SPI.
- Any other application, read through OCR.

Out of scope: games, the human's desktop, and OS tools.

Target applications on `archMachine` (from the installed `.desktop` files):

| Source | Applications                                                                     |
| ------ | -------------------------------------------------------------------------------- |
| CDP    | Google Chrome, VS Code, Cursor, Antigravity, figma-linux, balena-etcher          |
| AT-SPI | Firefox, Postman, Thunar, Wireshark, qBittorrent, pavucontrol, blueman           |
| OCR    | Zed, and any application with no accessibility tree                              |

## Success criteria

1. `snapshot` on VS Code, Chrome, Thunar and Wireshark returns the interactive elements with refs and screen coordinates.
2. `act` clicks and types on a ref, and returns a text diff of the change.
3. One scripted task per application (for example "open a file" or "toggle a setting") finishes with at most one screenshot.
4. `find` waits for an element to appear, so the agent does not need a blind `wait` followed by a screenshot.
5. Existing tools keep their behavior.

## Evidence from the spike on 2026-09-29 (verified)

- VS Code 1.128.0 (Electron 42.5.0) ran in a cage with `--remote-debugging-port=9333` and its own `--user-data-dir`. `/json/version` and `/json` answered.
- `Accessibility.getFullAXTree` on the workbench page returned 703 nodes, 71 of them interactive, in 26 ms.
- `devicePixelRatio` was 1 and the viewport was 1280x800.
- CDP box centres matched screen pixels. The "Continue without Signing In" button was at (991,650) through CDP and at (990,650) on a screenshot. The "Close" button was at (1090,145) through CDP and at (1090,144) on the screenshot.
- `tesseract` 5.5.3 is installed, but only `afr` and `osd` language data exist. OCR needs the `tesseract-data-eng` package (22.38 MiB).
- `at-spi2-core` 2.60.6 is installed. `org.a11y.Bus` runs on the session bus.

## Verified in task 1 (2026-09-29)

- **Shared bus: verified.** Caged applications join the human's single AT-SPI bus. `xdg-desktop-portal-gtk` from the human's session appeared next to them. Each caged application's PID carries `HYPRCAGE_SCREEN=<name>`, so the PID filter isolates it.
- **GTK3 coordinates: verified.** Thunar 4.20.9 buttons in window coordinates matched a screenshot exactly (Back at (22,48), Home at (133,48)).
- **GTK4 coordinates: verified, window type only.** pavucontrol 6.2 window extents matched within 5 px. Its screen extents are all (0,0), so the source uses the window coordinate type.
- **Qt6 coordinates: verified.** qBittorrent 5.2.3 "OK" was at (1229,776) through AT-SPI and at (1224,776) on the screenshot. It ran with `QT_LINUX_ACCESSIBILITY_ALWAYS_ON=1`. A run without the variable was not tested. The question is moot, because `app_launch` always sets it.
- **Hidden widgets: new fact.** GTK3 reports extents of (-2147483648, -2147483648, 1, 1) for hidden widgets. The source treats any negative-infinity extent as off screen.
- **Role names: new fact.** at-spi2-core 2.60 names push buttons `button`, not `push button`. Observed roles include `button`, `toggle button`, `radio button`, `check box`, `menu item`, `page tab`, `text`, `combo box`, `slider`, `table cell`, `label` and `alert`.
- **Electron apps: 5 of 6 verified.** VS Code, Cursor, Antigravity, figma-linux and balena-etcher answer on `--remote-debugging-port`. Postman ignores the flag (hypothesis: an Electron fuse).
- **Postman fallback: verified.** With `ACCESSIBILITY_ENABLED=1` and `--force-renderer-accessibility`, Postman publishes a 410-node AT-SPI tree. Without the variable it publishes none. Its AT-SPI coordinates are not verified yet.
- **Firefox: verified.** Firefox publishes a 449-node tree only with `GNOME_ACCESSIBILITY=1`. Back button coordinates matched a screenshot.
- **Zed: verified.** Zed publishes no AT-SPI tree, even with both variables. It uses OCR.
- **OCR: verified.** `tesseract-data-eng` is installed. On a VS Code screenshot, the words of "Continue without Signing In" had confidences of 79–88.

Changes that follow from these facts:

- `app_launch` always sets `QT_LINUX_ACCESSIBILITY_ALWAYS_ON=1`, `ACCESSIBILITY_ENABLED=1` and `GNOME_ACCESSIBILITY=1`.
- Postman moves from the CDP row to the AT-SPI row.
- The AT-SPI role table maps `button` as well as `push button`.

## Decisions

| Decision               | Choice                                                                                        | Reason                                                                                          |
| ---------------------- | --------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| Tool surface           | New tools inside hyprcage                                                                     | One surface handles many screens at once. `chrome-devtools-mcp` is fixed to one `--browserUrl`. |
| Source order           | CDP, then AT-SPI, then OCR                                                                    | CDP gives the richest tree. OCR works on anything but has no roles or states.                   |
| AT-SPI in v1           | Yes                                                                                           | The human asked for maximum accessibility coverage.                                             |
| Default snapshot       | Interactive elements, landmarks and headings                                                  | This keeps a reply at about 1–3k tokens. `full` mode is available on request.                   |
| Action input           | Existing virtual pointer and keyboard at the element centre                                   | Input stays silent and matches what a human does.                                               |
| AT-SPI fallback action | `Action.DoAction` when an element has no extents                                              | Some widgets publish no geometry.                                                               |
| OCR use                | Only when the screen has no CDP port and no AT-SPI application, or when the agent asks for it | OCR is slow and loses semantics.                                                                |
| DevTools port          | One free port per screen, bound to `127.0.0.1`                                                | Several screens run CDP applications at once.                                                   |

## Tool surface

### `app_launch` (changed)

A new parameter, `debug` (boolean, default `false`), applies to Chromium and Electron applications. When it is `true`, hyprcage does the following:

1. It picks a free TCP port on `127.0.0.1`.
2. It adds `--remote-debugging-port=<port>`, `--remote-debugging-address=127.0.0.1` and `--force-renderer-accessibility`.
3. It stores the port in the screen record.

`app_launch` also always sets `QT_LINUX_ACCESSIBILITY_ALWAYS_ON=1`, `ACCESSIBILITY_ENABLED=1` and `GNOME_ACCESSIBILITY=1`.

`browser_open` uses the same per-screen port. The fixed port `9222` stays the default only for `chrome-devtools-mcp`.

### `snapshot` (new)

Parameters:

- `screen` (optional when the session owns one screen).
- `mode`: `interactive` (default) or `full`.
- `root`: a ref. The reply covers only that subtree.
- `max_nodes`: a cap, default 300.
- `source`: `auto` (default), `cdp`, `atspi` or `ocr`.

The reply is plain text. The first line names the source and the node count:

```
source=cdp nodes=71 truncated=false
[e1] menuitem "File" (54,17)
[e12] button "Continue with GitHub" (656,388) focused
[e14] textbox "Search" value="" (640,40) required
[e20] treeitem "src" (80,210) expanded level=2
[e31] button "Run" (900,700) offscreen
[e40] menuitem "Open Parent" (action)
```

A node with `(action)` has no reliable coordinates. The AT-SPI source marks every node under a `menu` or `popup menu` object this way, because Wayland gives AT-SPI no popup origin. `act` clicks such a node through its accessibility action.

### `act` (new)

Parameters:

- `ref`: the element.
- `op`: `click`, `double_click`, `type`, `key`, `hover` or `scroll`.
- `text`: the text for `type`.
- `keys`: the combinations for `key`.
- `screenshot_after`: default `false`.

`act` does the following, in order:

1. It resolves the ref.
2. It scrolls the element into view when the element is off screen.
3. It sends the input at the element centre. For an `(action)` node, `click` and `double_click` call the accessibility action (AT-SPI `DoAction` 0). Every other op on an `(action)` node fails with `unsupported_input`.
4. It waits until the tree has not changed for 1 s before the first change, or for 300 ms after a change, with a timeout of 3 s.
5. It takes a new snapshot and returns only the diff.

The diff has three sections: added, removed and changed. A change is a new name, value or state.

### `find` (new)

Parameters:

- `text`: a regular expression on the name or value.
- `role`: an optional role filter.
- `timeout_ms`: default 0. A positive value waits until a match appears.

The reply lists the matching refs in the `snapshot` line format.

### Existing tools

`screenshot`, `click`, `type`, `key`, `scroll`, `drag`, `wait` and `windows` do not change.

## Architecture

The new package `internal/perceive` holds the sources and the shared model. Each source has its own file.

```
internal/perceive/
  node.go      Node model, text rendering, diff
  refs.go      ref table per screen, resolve, re-resolve
  cdp.go       CDP source: targets, auto-attach, AX tree, box model
  atspi.go     AT-SPI source: bus address, registry, PID filter, extents
  ocr.go       OCR source: screenshot, tesseract TSV, lines
  source.go    source choice
```

`internal/mcpserver/server_perceive.go` registers `snapshot`, `act` and `find`. CLI twins are `hyprcage snapshot`, `hyprcage act` and `hyprcage find`.

### Node model

A node has these fields: ref, role, name, value, description, level, states, the centre point in screen pixels, and an off-screen flag. States are `focused`, `checked`, `expanded`, `selected`, `disabled`, `required`, `invalid` and `readonly`.

### Refs

- A CDP ref maps to the pair (target ID, `backendDOMNodeId`).
- An AT-SPI ref maps to the pair (bus name, object path).
- An OCR ref (`o<n>`) maps to a line box. OCR refs are valid only until the next OCR snapshot.
- The same element keeps the same ref across snapshots. This makes the diff possible.
- The ref table lives in the MCP server process, one table per screen. The CLI twin keeps no table between calls, so its refs are valid only within one call.
- When a ref is stale, `act` looks once for one node with the same role and name. If it finds none, or more than one, `act` fails with `stale_ref`.

### CDP source

1. Read `/json` on the screen's port.
2. Attach to every `page` target. Call `Target.setAutoAttach` with `flatten: true` to reach iframes, out-of-process iframes and Electron webviews.
3. Call `Accessibility.getFullAXTree` for each attached target.
4. Call `DOM.getBoxModel` for each kept node to get its box.
5. Compute the centre as (box centre × `devicePixelRatio`) plus the offset of the frame.
6. Scroll with `DOM.scrollIntoViewIfNeeded`.

### AT-SPI source

1. Get the bus address from `org.a11y.Bus.GetAddress` on the session bus.
2. List the applications under the registry root, `org.a11y.atspi.Registry`.
3. Keep only the applications whose PID is in `screenProcesses(name)`. The PID comes from `GetConnectionUnixProcessID`.
4. Walk each tree. Read role, name, states and `Component.GetExtents` with the window coordinate type.
5. When an element has no extents, `act` uses `Action.DoAction` for `click`.

### OCR source

1. Capture the screen with the existing capture code.
2. Run `tesseract - - tsv` with the `eng` language data.
3. Group the words into lines. Each line becomes one node with the role `text`.

## Errors

| Code              | Meaning                                                                                     |
| ----------------- | ------------------------------------------------------------------------------------------- |
| `stale_ref`       | The ref no longer resolves, and re-resolution found no single match.                        |
| `no_source`       | The screen has no CDP port and no AT-SPI application, and the OCR language data is missing. |
| `ref_offscreen`   | The element is off screen, and scrolling did not bring it into view.                        |
| `ref_occluded`    | Another element covers the element centre (CDP hit-test). Close the dialog or act on the covering element. |
| `unsupported_input` | The source cannot do the op on this element, for example `hover` on an `(action)` node.     |
| `cdp_unreachable` | The screen has a port, but CDP does not answer.                                             |

A snapshot that reaches `max_nodes` sets `truncated=true` in its first line. It is not an error.

## Dependencies

- `github.com/godbus/dbus/v5` for AT-SPI.
- One websocket library for CDP. The plan chooses it.
- The `tesseract-data-eng` package on Arch, and the equivalent package on openSUSE. `install.sh` and `hyprcage setup` install it.
- `hyprcage doctor` checks the AT-SPI bus, the OCR language data, and CDP for a running screen.

## Testing

- **Unit tests** (Go): tree to text, diff, ref resolution and re-resolution, and OCR TSV to lines. Recorded CDP and AT-SPI fixtures drive them.
- **Smoke script**: for each of VS Code, Chrome, Thunar and Wireshark, create a screen, launch the application, run `snapshot`, and run `act` on a known element. The step passes when the diff shows the expected change.
- **E2E**: run the scripted task for each application in a hyprcage screen with the mirror open. Count the screenshots. Criterion 3 requires at most one per task.
