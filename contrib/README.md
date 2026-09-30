# Desktop integration (mrwick1 fork)

These files are optional. Each machine links them in from its own configuration.

## Waybar

1. Link the scripts into your `PATH`:

   ```sh
   ln -s "$PWD/contrib/waybar/hyprcage-agents" ~/.local/bin/
   ln -s "$PWD/contrib/waybar/hyprcage-menu" ~/.local/bin/
   ```

2. Add the module to `~/.config/waybar/config.jsonc`, and add `"custom/agents"` to a modules list:

   ```jsonc
   "custom/agents": {
     "exec": "hyprcage-agents",
     "return-type": "json",
     "interval": 2,
     "on-click": "hyprcage-menu"
   }
   ```

3. Add the style to `~/.config/waybar/style.css`:

   ```css
   #custom-agents { padding: 0 8px; }
   #custom-agents.recording-desktop { color: #ef5f6b; }
   ```

## Chrome control

Nothing to register. `browser_open` starts Chrome on a screen with DevTools on a
free 127.0.0.1 port of its own. `snapshot`, `act` and `find` drive the page. The
`devtools_eval`, `devtools_console`, `devtools_trace` and `devtools_heap` tools
read its internals. They also work on any app launched with `debug=true`.

Every devtools tool checks that the port is still held by the process that opened
it, so the tools never reach the human's own Chrome.

The `browser.port` config key is ignored. Older config files that still set it
load without an error.
