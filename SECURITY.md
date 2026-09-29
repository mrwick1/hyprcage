# Security

What hyprcage isolates, and what it does not.

## What it does

- The agent's clicks and keystrokes go to a nested compositor with its own
  seat. They cannot land on the human's windows by accident.
- The agent's windows open on a headless output that no physical screen shows.
  They cannot steal the human's focus, cursor or workspaces.
- Screens are closed when the agent asks, when its session ends, and at the
  latest 15 minutes after its session is found dead.

## What it does not do

1. **It is not a sandbox.** An application launched with `app_launch` runs as
   your user, with your files, SSH keys, tokens and browser profiles.
   `app_launch` is arbitrary code execution, exactly like a shell.
2. **The graphical isolation is one environment variable deep.** The
   application sees `$XDG_RUNTIME_DIR` and can connect to the human's own
   compositor socket (`wayland-N`, Hyprland's `.socket.sock`) and inject
   input there, despite hyprcage removing `HYPRLAND_INSTANCE_SIGNATURE` from
   its environment. Only a real sandbox (bubblewrap, a mount namespace hiding
   `$XDG_RUNTIME_DIR` except cage's socket) would prevent that. It is out of
   scope for now.
3. **State is shared within the user.** The registry
   (`$XDG_RUNTIME_DIR/hyprcage`) and cage's socket are readable by every
   process of the user, including the agent's applications: an agent can list
   the screens of other sessions and could capture or inject into any cage.
   Ownership checks (`not_owner`) protect against mistakes, not against a
   malicious agent with a shell.

If you need to contain an agent, contain the agent: run it in a VM or a
container, and give it hyprcage inside.

## What the installer and the plugin do

- `install.sh` and the plugin's launcher download the release binary over
  HTTPS from this repository's GitHub releases and verify it against the
  `SHA256SUMS` published with the same release. That protects against
  corruption and against a tampered download, not against a compromised
  GitHub account. Signed releases are planned.
- Root is used for one thing: the package install of cage, ffmpeg and
  wl-clipboard (`pacman -S` or `zypper install`), through `sudo`
  when a terminal is there or through `pkexec`, which opens your desktop's
  own authentication dialog. Nothing else runs as root, and `hyprcage setup`
  prints the exact command if you prefer to run it yourself.
- `hyprcage` and everything it starts run as your user. The plugin adds a
  SessionStart and a SessionEnd hook that only record the session and
  schedule the cleanup of its screens.

## Reporting

Open a private security advisory on GitHub or write to the maintainer.
