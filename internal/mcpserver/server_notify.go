package mcpserver

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/hyprcage/internal/notifyd"
	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/setup"
)

type notifyListIn struct {
	Since string `json:"since,omitempty" jsonschema:"only notifications from this time on: RFC3339, or a duration back from now such as 2h"`
	App   string `json:"app,omitempty" jsonschema:"only this app name (exact)"`
	Match string `json:"match,omitempty" jsonschema:"regular expression on the summary and body"`
}

type notifyActIn struct {
	ID     uint32 `json:"id" jsonschema:"notification ID from notify_list"`
	Action string `json:"action,omitempty" jsonschema:"action key to invoke; without it, the notification closes"`
}

type notifyWaitIn struct {
	App       string `json:"app,omitempty" jsonschema:"only this app name (exact)"`
	Match     string `json:"match,omitempty" jsonschema:"regular expression on the summary and body"`
	TimeoutMs int    `json:"timeout_ms" jsonschema:"give up after this long, at most 600000"`
}

// registerNotify adds the tools that read and act on the human's notifications.
func (s *Server) registerNotify(srv *mcp.Server) {
	tool(s, srv, "notify_list", "List the human's desktop notifications of the last 48 hours, newest first, with ID, app, summary, body, actions (key, label pairs), time and closed flag.", s.notifyList)
	tool(s, srv, "notify_act", "Close a notification, or with action invoke one of its actions. An action works only on the latest open notification; otherwise the tool refuses with no_action.", s.notifyAct)
	unlocked(srv, "notify_wait", "Wait for a new notification that matches app and match, and return it.", s.notifyWait)
}

// notifications reads the notification file. With unit, it also requires
// the daemon's unit to run, unless HYPRCAGE_NOTIFY_FILE names the file.
func notifications(unit bool) ([]notifyd.Entry, error) {
	const hint = "systemctl --user start hyprcage-notifyd"
	if unit && os.Getenv("HYPRCAGE_NOTIFY_FILE") == "" {
		out, _ := exec.Command("systemctl", "--user", "is-active", setup.NotifydUnit).Output()
		if state := strings.TrimSpace(string(out)); state != "active" {
			return nil, screen.Errf(screen.CodeNotifydDown, hint, "%s is %s", setup.NotifydUnit, state)
		}
	}
	entries, err := notifyd.Read(notifyd.Path())
	if errors.Is(err, os.ErrNotExist) {
		return nil, screen.Errf(screen.CodeNotifydDown, hint, "no notification file at %s", notifyd.Path())
	}
	return entries, err
}

func compileMatch(match string) (*regexp.Regexp, error) {
	if match == "" {
		return nil, nil
	}
	re, err := regexp.Compile(match)
	if err != nil {
		return nil, fmt.Errorf("match is not a valid regular expression: %v", err)
	}
	return re, nil
}

func (s *Server) notifyList(in notifyListIn) (*mcp.CallToolResult, error) {
	var since time.Time
	if in.Since != "" {
		if d, err := time.ParseDuration(in.Since); err == nil {
			since = time.Now().Add(-d)
		} else if since, err = time.Parse(time.RFC3339, in.Since); err != nil {
			return nil, fmt.Errorf("since is neither RFC3339 nor a duration: %q", in.Since)
		}
	}
	re, err := compileMatch(in.Match)
	if err != nil {
		return nil, err
	}
	entries, err := notifications(true)
	if err != nil {
		return nil, err
	}
	return textResult(notifyd.List(entries, since, in.App, re)), nil
}

func (s *Server) notifyAct(in notifyActIn) (*mcp.CallToolResult, error) {
	entries, err := notifications(false)
	if err != nil {
		return nil, err
	}
	obj, path, call, arg := "org.freedesktop.Notifications", "/org/freedesktop/Notifications", "org.freedesktop.Notifications.CloseNotification", in.ID
	if in.Action != "" {
		n, ok := notifyd.Latest(entries)
		if !ok || n.ID != in.ID {
			return nil, screen.Errf(screen.CodeNoAction, "only the latest open notification takes an action; see notify_list", "notification %d is not the latest open one", in.ID)
		}
		// swaync takes the index of the key among the keys in Notify order,
		// without the "default" key, which it keeps apart and cannot invoke.
		idx := -1
		for i, k := 0, 0; i < len(n.Actions); i += 2 {
			if n.Actions[i] == "default" {
				continue
			}
			if n.Actions[i] == in.Action {
				idx = k
				break
			}
			k++
		}
		if idx < 0 {
			return nil, screen.Errf(screen.CodeNoAction, "use a key from the actions of notify_list", "notification %d has no action %q", in.ID, in.Action)
		}
		obj, path, call, arg = "org.erikreider.swaync.cc", "/org/erikreider/swaync/cc", "org.erikreider.swaync.cc.LatestInvokeAction", uint32(idx)
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err := conn.Object(obj, dbus.ObjectPath(path)).Call(call, 0, arg).Err; err != nil {
		return nil, err
	}
	return textResult(map[string]string{"status": "ok"}), nil
}

func (s *Server) notifyWait(in notifyWaitIn) (*mcp.CallToolResult, error) {
	re, err := compileMatch(in.Match)
	if err != nil {
		return nil, err
	}
	if _, err := notifications(true); err != nil {
		return nil, err
	}
	start := time.Now()
	timeout := min(time.Duration(max(in.TimeoutMs, 0))*time.Millisecond, 10*time.Minute)
	var size int64 = -1
	for {
		if fi, err := os.Stat(notifyd.Path()); err == nil && fi.Size() != size {
			size = fi.Size()
			entries, err := notifyd.Read(notifyd.Path())
			if err != nil {
				return nil, err
			}
			if ns := notifyd.List(entries, start, in.App, re); len(ns) > 0 {
				return textResult(ns[len(ns)-1]), nil // the first one after start
			}
		}
		if time.Since(start) >= timeout {
			return nil, screen.Errf(screen.CodeTimeout, "", "no matching notification within %s", timeout)
		}
		time.Sleep(250 * time.Millisecond)
	}
}
