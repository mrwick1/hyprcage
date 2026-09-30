#!/usr/bin/env python3
"""E2E test of desktop control over MCP stdio, against a nested Hyprland only.

The script starts a nested Hyprland with tests/nested-hypr.sh and re-runs
itself under dbus-run-session. In that isolated session bus it starts
swaync (on the nested instance), `hyprcage notifyd` and `hyprcage mcp`, and
drives the MCP server step by step. Before and after every step it reads the
active workspace and the cursor position of the human's instance: any change
fails the run (success criterion 6). It never dispatches to the human's
instance and never sends a notification to the human's session bus.

    HC=/path/to/hyprcage python3 tests/e2e-desktop.py
"""
import json
import os
import re
import subprocess
import sys
import tempfile
import time

HERE = os.path.dirname(os.path.abspath(__file__))
HC = os.environ.get("HC", "hyprcage")
CHILD = "HC_E2E_CHILD"


def hyprctl(sig, *args):
    """Run hyprctl. sig=None keeps the inherited (human's) signature: read only."""
    env = dict(os.environ)
    if sig is not None:
        env["HYPRLAND_INSTANCE_SIGNATURE"] = sig
    else:
        env["HYPRLAND_INSTANCE_SIGNATURE"] = os.environ["HC_E2E_HUMAN_SIG"]
    return subprocess.run(["hyprctl", *args], env=env, capture_output=True, text=True, timeout=10).stdout


# ---------------------------------------------------------------- parent


def parent():
    human = os.environ.get("HYPRLAND_INSTANCE_SIGNATURE")
    if not human:
        sys.exit("HYPRLAND_INSTANCE_SIGNATURE is not set: run inside the human's Hyprland session")
    os.environ["HC_E2E_HUMAN_SIG"] = human
    sig = subprocess.run([f"{HERE}/nested-hypr.sh", "start"], capture_output=True, text=True, check=True).stdout.strip()
    print(f"nested Hyprland: {sig}")
    try:
        sock = next(i["wl_socket"] for i in json.loads(hyprctl(sig, "instances", "-j")) if i["instance"] == sig)
        env = dict(os.environ, **{
            CHILD: sig,
            # Everything in the isolated session (swaync, a D-Bus activated
            # service, the apps) draws on the nested instance only.
            "WAYLAND_DISPLAY": sock,
            "HYPRCAGE_DESKTOP_INSTANCE": sig,
        })
        env.pop("DISPLAY", None)
        # The isolated bus daemon and the services it starts write logs to
        # its stdout and stderr, so these go to a file. The child writes the
        # results to a copy of the terminal's stdout.
        out = os.dup(1)
        env["HC_E2E_OUT"] = str(out)
        with tempfile.NamedTemporaryFile("w", prefix="hc-e2e-dbus.", suffix=".log") as log:
            return subprocess.run(["dbus-run-session", "--", sys.executable, os.path.abspath(__file__)],
                                  env=env, stdout=log, stderr=log, pass_fds=(out,)).returncode
    finally:
        subprocess.run([f"{HERE}/nested-hypr.sh", "stop", sig])
        for _ in range(50):  # the exit dispatch returns before the instance ends
            if not os.path.exists(f"{os.environ['XDG_RUNTIME_DIR']}/hypr/{sig}/.socket.sock"):
                break
            time.sleep(0.2)


# ---------------------------------------------------------------- MCP client


class MCP:
    def __init__(self, env):
        self.p = subprocess.Popen([HC, "mcp"], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                  stderr=open(env["HC_E2E_TMP"] + "/mcp.log", "w"), text=True, env=env)
        self.id = 0
        self.shots = 0
        self.request("initialize", {"protocolVersion": "2025-06-18", "capabilities": {},
                                    "clientInfo": {"name": "e2e-desktop", "version": "1"}})
        self.notify("notifications/initialized")

    def send(self, obj):
        self.p.stdin.write(json.dumps(obj) + "\n")
        self.p.stdin.flush()

    def notify(self, method):
        self.send({"jsonrpc": "2.0", "method": method})

    def start(self, method, params):
        self.id += 1
        self.send({"jsonrpc": "2.0", "id": self.id, "method": method, "params": params})
        return self.id

    def wait(self, rid):
        while True:
            line = self.p.stdout.readline()
            if not line:
                raise RuntimeError("MCP server closed stdout")
            msg = json.loads(line)
            if msg.get("id") == rid:
                if "error" in msg:
                    raise RuntimeError(f"JSON-RPC error: {msg['error']}")
                return msg["result"]

    def request(self, method, params):
        return self.wait(self.start(method, params))

    def call_start(self, name, **args):
        return self.start("tools/call", {"name": name, "arguments": args})

    def call_wait(self, rid):
        """Returns (is_error, text, image count)."""
        r = self.wait(rid)
        text = "\n".join(c.get("text", "") for c in r.get("content", []) if c.get("type") == "text")
        images = sum(1 for c in r.get("content", []) if c.get("type") == "image")
        self.shots += images
        return bool(r.get("isError")), text, images

    def call(self, name, **args):
        return self.call_wait(self.call_start(name, **args))

    def close(self):
        try:
            self.p.stdin.close()
            self.p.wait(5)
        except Exception:
            self.p.kill()


# ---------------------------------------------------------------- steps


class Fail(Exception):
    pass


def need(cond, msg):
    if not cond:
        raise Fail(msg)


def ok(res, what):
    err, text, _ = res
    need(not err, f"{what}: {text[:300]}")
    return text


NODE = re.compile(r'^\[(\w+)\] (\S+) "((?:[^"\\]|\\.)*)"(?: value="(?:[^"\\]|\\.)*")? \((?:(-?\d+),(-?\d+)|action)\)')


def nodes(snapshot):
    """(ref, role, name, x, y) of every element line; x and y are None for an action-only element."""
    out = []
    for line in snapshot.splitlines():
        m = NODE.match(line)
        if m:
            out.append((m[1], m[2], m[3], m[4] and int(m[4]), m[5] and int(m[5])))
    return out


def child():
    out = int(os.environ["HC_E2E_OUT"])
    os.dup2(out, 1)
    os.dup2(out, 2)
    sig = os.environ[CHILD]
    tmp = tempfile.mkdtemp(prefix="hc-e2e-desktop.")
    notify_file = f"{tmp}/notifications.jsonl"
    env = dict(os.environ, HC_E2E_TMP=tmp, HYPRCAGE_NOTIFY_FILE=notify_file)
    bus = os.environ["DBUS_SESSION_BUS_ADDRESS"]
    procs = []
    results = []
    mcp = None

    def nested(*args):
        return hyprctl(sig, *args)

    def nested_ws():
        return json.loads(nested("activeworkspace", "-j"))["id"]

    def nested_cursor():
        return nested("cursorpos").strip()

    def human():
        return (json.loads(hyprctl(None, "activeworkspace", "-j"))["id"], hyprctl(None, "cursorpos").strip())

    def client(addr):
        return next((c for c in json.loads(nested("clients", "-j")) if c["address"] == addr), None)

    def inside(addr, x, y):
        c = client(addr)
        (cx, cy), (w, h) = c["at"], c["size"]
        return cx <= x < cx + w and cy <= y < cy + h

    state = {}

    def step(name, fn):
        before = human()
        status, reason = "PASS", ""
        try:
            reason = fn() or ""
        except Fail as e:
            status, reason = "FAIL", str(e)
        except Skip as e:
            status, reason = "SKIP", str(e)
        except Exception as e:  # a script or protocol error still fails the step
            status, reason = "FAIL", f"{type(e).__name__}: {e}"
        after = human()
        if after != before:
            status, reason = "FAIL", f"human's instance changed: {before} -> {after}; {reason}"
        results.append(status)
        print(f"{status} [{name}] {reason}", flush=True)

    try:
        procs.append(subprocess.Popen(["swaync"], env=env, stdout=subprocess.DEVNULL, stderr=open(f"{tmp}/swaync.log", "w")))
        for _ in range(50):
            r = subprocess.run(["dbus-send", "--session", "--print-reply", "--dest=org.freedesktop.DBus",
                                "/org/freedesktop/DBus", "org.freedesktop.DBus.GetNameOwner",
                                "string:org.freedesktop.Notifications"], capture_output=True, text=True)
            if r.returncode == 0:
                break
            time.sleep(0.2)
        else:
            raise SystemExit("swaync did not take org.freedesktop.Notifications")
        procs.append(subprocess.Popen([HC, "notifyd"], env=env, stdout=subprocess.DEVNULL, stderr=open(f"{tmp}/notifyd.log", "w")))
        # notifyd creates the file on its first event only, and the notify_*
        # tools refuse with notifyd_down while it is missing.
        open(notify_file, "a").close()
        # at-spi-bus-launcher starts the a11y bus of the isolated session on
        # the first GetAddress. Its registry needs systemd activation, which
        # the isolated bus does not have, so the script starts it itself.
        subprocess.run(["dbus-send", "--session", "--print-reply", "--dest=org.a11y.Bus", "/org/a11y/bus",
                        "org.a11y.Bus.GetAddress"], capture_output=True, check=True)
        procs.append(subprocess.Popen(["/usr/lib/at-spi2-registryd"], env=env, stdout=subprocess.DEVNULL,
                                      stderr=open(f"{tmp}/registryd.log", "w")))
        # The nested instance starts on workspace 2 (its headless output), and
        # the steps expect workspace 1 as the visible one.
        nested("dispatch", "workspace", "1")
        mcp = MCP(env)
        # The apps join the isolated bus, so that their AT-SPI tree is on the
        # a11y bus of the session that the MCP server reads.
        app_env = {"DBUS_SESSION_BUS_ADDRESS": bus}

        def launch(cmd, ws, extra=None):
            text = ok(mcp.call("app_launch", screen="desktop", command=cmd, workspace=ws,
                               env={**app_env, **(extra or {})}, wait_window_ms=20000), f"app_launch {cmd[0]}")
            addr = json.loads(text).get("window")
            need(addr, f"app_launch {cmd[0]}: no window: {text}")
            return addr

        def a_launch():
            state["thunar"] = launch(["thunar", "/tmp"], 1)
            state["wireshark"] = launch(["wireshark"], 1, {"QT_LINUX_ACCESSIBILITY_ALWAYS_ON": "1"})
            state["kitty"] = launch(["kitty", "--class", "hc-e2e-hidden"], 4)
            need(nested_ws() == 1, f"nested active workspace is {nested_ws()}, not 1")
            need(client(state["kitty"])["workspace"]["id"] == 4, "kitty is not on workspace 4")
            return f"thunar={state['thunar']} wireshark={state['wireshark']} kitty={state['kitty']}"

        def snap(addr, **kw):
            return ok(mcp.call("snapshot", screen="desktop", window=addr, **kw), "snapshot")

        def b_snapshot():
            t = state["thunar"]
            text = ""
            for _ in range(20):  # the app joins the a11y bus some time after its window maps
                text = snap(t)
                if any(n[2] == "Home" for n in nodes(text)):
                    break
                time.sleep(0.5)
            need(text.startswith("source=atspi"), f"header: {text.splitlines()[0]}")
            home = [n for n in nodes(text) if n[2] == "Home" and n[1] in ("button", "pushbutton", "togglebutton", "menuitem", "listitem")]
            home = home or [n for n in nodes(text) if n[2] == "Home"]
            need(home, "no Home element in the snapshot")
            h = home[0]
            need(inside(t, h[3], h[4]), f"Home at ({h[3]},{h[4]}) is outside Thunar {client(t)['at']}+{client(t)['size']}")
            state["home"], state["thunar_snap"] = h[0], text
            return f"{text.splitlines()[0]}; Home {h[1]} [{h[0]}] at ({h[3]},{h[4]})"

        def c_click_home():
            cur = nested_cursor()
            text = ok(mcp.call("act", screen="desktop", window=state["thunar"], ref=state["home"], op="click"), "act click Home")
            need(text.splitlines()[0] == "path=atspi", f"first line: {text.splitlines()[0]}")
            need(nested_cursor() == cur, f"nested cursor moved: {cur} -> {nested_cursor()}")
            return f"path=atspi, nested cursor stays at {cur}"

        def d_type():
            cur = nested_cursor()
            addr, text = state["thunar"], snap(state["thunar"])
            box = [n for n in nodes(text) if n[1] in ("textbox", "entry", "text")]
            where = "Thunar location"
            if not box:
                addr, where = state["wireshark"], "Wireshark display filter"
                text = ok(mcp.call("find", screen="desktop", window=addr, text="Display filter", timeout_ms=15000), "find display filter")
                box = [n for n in nodes(text) if "Display filter" in n[2]] or nodes(text)
            need(box, f"no textbox in Thunar nor Wireshark: {text[:200]}")
            res = ok(mcp.call("act", screen="desktop", window=addr, ref=box[0][0], op="type", text="e2e"), f"act type {where}")
            need(res.splitlines()[0] == "path=atspi", f"first line: {res.splitlines()[0]}")
            need(nested_cursor() == cur, f"nested cursor moved: {cur} -> {nested_cursor()}")
            return f"{where} [{box[0][0]}] {box[0][1]}: path=atspi, cursor unchanged"

        def e_about():
            w = state["wireshark"]
            text = ok(mcp.call("find", screen="desktop", window=w, text="^Help$", timeout_ms=15000), "find Help")
            h = [n for n in nodes(text) if n[2] == "Help"]
            need(h, f"no Help node: {text[:200]}")
            r = ok(mcp.call("act", screen="desktop", window=w, ref=h[0][0], op="click"), "act click Help")
            p1 = r.splitlines()[0]
            text = ok(mcp.call("find", screen="desktop", window=w, text="^About", timeout_ms=5000), "find About")
            a = [n for n in nodes(text) if n[2].startswith("About")]
            problem = ""
            if not a:
                # The act did not open the menu. Record it, then open the menu
                # with a pointer click to check the rest of the path.
                problem = f"act click Help gave {r[:60]!r} and no About item; "
                ok(mcp.call("click", screen="desktop", window=w, x=h[0][3], y=h[0][4]), "pointer click Help")
                text = ok(mcp.call("find", screen="desktop", window=w, text="^About", timeout_ms=5000), "find About")
                a = [n for n in nodes(text) if n[2].startswith("About")]
                if not a:
                    ok(mcp.call("desktop_key", address=w, keys=["Escape"]), "desktop_key Escape")
                    raise Fail(problem + "no About item after a pointer click on Help either")
            before = {c["address"] for c in json.loads(nested("clients", "-j"))}
            r = ok(mcp.call("act", screen="desktop", window=w, ref=a[0][0], op="click"), "act click About")
            p2 = r.splitlines()[0]
            dialog = None
            for _ in range(40):
                new = [c for c in json.loads(nested("clients", "-j")) if c["address"] not in before]
                dialog = next((c for c in new if "About" in c["title"]), None) or (new[0] if new else None)
                if dialog:
                    break
                time.sleep(0.25)
            need(dialog, "no dialog window appeared")
            ok(mcp.call("desktop_key", address=dialog["address"], keys=["Escape"]), "desktop_key Escape")
            for _ in range(20):
                if not client(dialog["address"]):
                    break
                time.sleep(0.25)
            need(not client(dialog["address"]), f"dialog {dialog['title']!r} is still open after Escape")
            summary = f"About {p2}, dialog {dialog['title']!r} opened and closed with Escape"
            need(not problem, problem + "after a pointer click on Help: " + summary)
            return f"Help {p1}, " + summary

        def f_ocr():
            t = state["thunar"]
            text = snap(t, source="ocr")
            need(text.startswith("source=ocr"), f"header: {text.splitlines()[0]}")
            # OCR nodes are lines of text; the menu bar is one line.
            go = [n for n in nodes(text) if n[2] == "Go"] or [n for n in nodes(text) if re.search(r"\bGo\b", n[2])]
            need(go, f"no OCR word 'Go': {[n[2] for n in nodes(text)][:30]}")
            cur = nested_cursor()
            r = ok(mcp.call("act", screen="desktop", window=t, ref=go[0][0], op="click"), "act click Go")
            first = r.splitlines()[0]
            after = nested_cursor()
            ok(mcp.call("desktop_key", address=t, keys=["Escape"]), "desktop_key Escape")
            need(first == "path=pointer", f"first line: {first}")
            need(after == cur, f"nested cursor not restored: {cur} -> {after}")
            return f"Go [{go[0][0]}] path=pointer, cursor back at {cur}"

        def g_hidden_shot():
            ws = nested_ws()
            need(ws != 4, "nested workspace 4 is active before the step")
            n0 = mcp.shots
            err, text, imgs = mcp.call("screenshot", screen="desktop", window=state["kitty"])
            need(not err, f"screenshot: {text[:300]}")
            need(imgs == 1, f"{imgs} images returned")
            need(nested_ws() == ws, f"nested workspace changed: {ws} -> {nested_ws()}")
            return f"1 image ({mcp.shots - n0} counted), nested workspace stays {ws}"

        def h_hidden_click():
            c = client(state["kitty"])
            x, y = c["at"][0] + c["size"][0] // 2, c["at"][1] + c["size"][1] // 2
            err, text, _ = mcp.call("click", screen="desktop", window=state["kitty"], x=x, y=y)
            need(err and "window_hidden" in text, f"expected window_hidden, got error={err}: {text[:300]}")
            return text.splitlines()[0][:120]

        def i_workspace():
            ok(mcp.call("desktop_workspace", workspace=4), "desktop_workspace 4")
            need(nested_ws() == 4, f"nested workspace is {nested_ws()} after desktop_workspace 4")
            ok(mcp.call("desktop_workspace", workspace=1), "desktop_workspace 1")
            need(nested_ws() == 1, f"nested workspace is {nested_ws()} after desktop_workspace 1")
            return "nested workspace 1 -> 4 -> 1"

        def j_notify():
            rid = mcp.call_start("notify_wait", match="e2e-probe", timeout_ms=20000)
            time.sleep(1)
            ns = subprocess.Popen(["notify-send", "--action=ok=OK", "--wait", "e2e-probe", "body"],
                                  env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            procs.append(ns)
            got = json.loads(ok(mcp.call_wait(rid), "notify_wait"))
            need(got.get("summary") == "e2e-probe", f"notify_wait returned {got}")
            nid = got["id"]
            lst = json.loads(ok(mcp.call("notify_list", match="e2e-probe"), "notify_list"))
            need(any(n["id"] == nid for n in lst), f"notify_list lacks id {nid}: {lst}")
            ok(mcp.call("notify_act", id=nid, action="ok"), "notify_act action ok")
            try:
                out, errout = ns.communicate(timeout=10)
            except subprocess.TimeoutExpired:
                raise Fail("notify-send --wait did not return after notify_act action ok")
            need(out.strip() == "ok", f"notify-send printed {out!r} {errout!r}")
            nid2 = int(subprocess.run(["notify-send", "-p", "e2e-close", "body2"], env=env,
                                      capture_output=True, text=True, check=True).stdout.strip())
            for _ in range(20):
                lst = json.loads(ok(mcp.call("notify_list", match="e2e-close"), "notify_list"))
                if any(n["id"] == nid2 for n in lst):
                    break
                time.sleep(0.25)
            ok(mcp.call("notify_act", id=nid2), "notify_act close")
            closed = None
            for _ in range(20):
                lst = json.loads(ok(mcp.call("notify_list", match="e2e-close"), "notify_list"))
                closed = next((n.get("closed") for n in lst if n["id"] == nid2), None)
                if closed:
                    break
                time.sleep(0.25)
            need(closed is True, f"notification {nid2} not closed: {lst}")
            return f"wait/list/act ok on {nid}, notify-send printed 'ok'; {nid2} closed"

        def k_lock():
            raise Skip("cannot lock a session safely in an unattended run (criterion 5 needs hyprlock)")

        for name, fn in [("a app_launch", a_launch), ("b snapshot atspi", b_snapshot), ("c act click Home", c_click_home),
                         ("d act type", d_type), ("e Wireshark About", e_about), ("f OCR pointer click", f_ocr),
                         ("g hidden screenshot", g_hidden_shot), ("h hidden click", h_hidden_click),
                         ("i desktop_workspace", i_workspace), ("j notifications", j_notify), ("k lock refusal", k_lock)]:
            if name[0] in "bcdefgh" and not state.get("kitty"):
                results.append("FAIL")
                print(f"FAIL [{name}] no windows from step a", flush=True)
                continue
            step(name, fn)
    finally:
        if mcp:
            mcp.close()
        for p in procs:
            if p.poll() is None:
                p.terminate()
                try:
                    p.wait(5)
                except subprocess.TimeoutExpired:
                    p.kill()
        subprocess.run(["rm", "-rf", tmp])
    passed, failed, skipped = results.count("PASS"), results.count("FAIL"), results.count("SKIP")
    print(f"summary: {passed} passed, {failed} failed, {skipped} skipped; screenshots used: {mcp.shots if mcp else 0}")
    return 1 if failed else 0


class Skip(Exception):
    pass


if __name__ == "__main__":
    sys.exit(child() if os.environ.get(CHILD) else parent())
