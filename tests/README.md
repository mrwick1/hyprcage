# Tests

- Install the pinned Bare release with `bash install.sh --bare-only` before
  browser integration tests. Tests skip the engine checks when Bare is absent.
- `go test $(go list ./... | rg -v '/internal/headless$')` runs the current
  tests without launching Chrome. The legacy headless package still tests the
  former Chrome backend for compatibility code.
- Build offstage, then set `OFFSTAGE_TEST_BINARY` to its absolute path and run
  `go test ./internal/mcpserver -run TestBareVisible -v` on Hyprland to check a
  real cage, a visible Bare page and screen teardown.
- `bash tests/install-bare-required.sh` checks missing downloads, checksum
  rejection and driver repair with local assets.
- `bash tests/plugin-launcher.sh` checks plugin updates with local assets.
- `tests/acceptance.sh` (to come) — plays the acceptance criteria A1–A13 of
  the specification inside a Hyprland VM. Never run it on a personal machine:
  it creates outputs, launches compositors and kills processes.
