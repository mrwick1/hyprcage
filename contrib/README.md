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

Register the Chrome DevTools MCP once per machine:

```sh
claude mcp add --scope user agent-chrome -- npx -y chrome-devtools-mcp@latest --browserUrl http://127.0.0.1:9222
```

The port in `--browserUrl` must be the `browser.port` of the hyprcage config
(default 9222). When you change `browser.port`, register agent-chrome again
with the new port.
