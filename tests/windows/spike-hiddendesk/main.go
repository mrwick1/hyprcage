//go:build windows

// Spike B: can a hidden desktop (CreateDesktop) serve as an agent screen?
// It starts apps on a new desktop, captures their windows, sends them input
// and reads them with UI Automation. It also checks that the cursor and the
// foreground window of the input desktop stay the same.
// Run it inside the interactive session, not from an SSH session.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")

	pCreateDesktop       = user32.NewProc("CreateDesktopW")
	pCloseDesktop        = user32.NewProc("CloseDesktop")
	pSetThreadDesktop    = user32.NewProc("SetThreadDesktop")
	pEnumDesktopWindows  = user32.NewProc("EnumDesktopWindows")
	pEnumChildWindows    = user32.NewProc("EnumChildWindows")
	pIsWindowVisible     = user32.NewProc("IsWindowVisible")
	pGetWindowText       = user32.NewProc("GetWindowTextW")
	pGetClassName        = user32.NewProc("GetClassNameW")
	pGetWindowRect       = user32.NewProc("GetWindowRect")
	pGetWindowThreadPID  = user32.NewProc("GetWindowThreadProcessId")
	pPrintWindow         = user32.NewProc("PrintWindow")
	pGetDC               = user32.NewProc("GetDC")
	pReleaseDC           = user32.NewProc("ReleaseDC")
	pPostMessage         = user32.NewProc("PostMessageW")
	pSendMessage         = user32.NewProc("SendMessageW")
	pGetCursorPos        = user32.NewProc("GetCursorPos")
	pGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	pCreateCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")
	pCreateDIBSection    = gdi32.NewProc("CreateDIBSection")
	pSelectObject        = gdi32.NewProc("SelectObject")
	pDeleteObject        = gdi32.NewProc("DeleteObject")
	pDeleteDC            = gdi32.NewProc("DeleteDC")
)

const (
	genericAll          = 0x10000000
	pwRenderFullContent = 2
	wmChar              = 0x0102
	wmGetText           = 0x000D
	wmGetTextLength     = 0x000E
)

type rect struct{ L, T, R, B int32 }

type win struct {
	HWND     uintptr `json:"hwnd"`
	Title    string  `json:"title"`
	Class    string  `json:"class"`
	PID      uint32  `json:"pid"`
	W        int32   `json:"w"`
	H        int32   `json:"h"`
	Shot     string  `json:"shot,omitempty"`
	PrintOK  bool    `json:"print_ok"`
	NonBlack float64 `json:"non_black"`
	ShotMS   int64   `json:"shot_ms"`
}

type report struct {
	Desktop       string            `json:"desktop"`
	Launched      map[string]string `json:"launched"`
	Windows       []win             `json:"windows"`
	Input         map[string]string `json:"input"`
	ChromeKids    []string          `json:"chrome_children"`
	CursorBefore  [2]int32          `json:"cursor_before"`
	CursorAfter   [2]int32          `json:"cursor_after"`
	FgBefore      uintptr           `json:"foreground_before"`
	FgAfter       uintptr           `json:"foreground_after"`
	UIA           string            `json:"uia"`
	LeftoverProcs []string          `json:"leftover"`
	Errors        []string          `json:"errors"`
}

func u16(s string) *uint16 { p, _ := windows.UTF16PtrFromString(s); return p }

func text(h uintptr, p *windows.LazyProc) string {
	b := make([]uint16, 512)
	p.Call(h, uintptr(unsafe.Pointer(&b[0])), 512)
	return windows.UTF16ToString(b)
}

func cursor() [2]int32 {
	var pt [2]int32
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	return pt
}

func fg() uintptr { h, _, _ := pGetForegroundWindow.Call(); return h }

// onInputDesktop reads the cursor and the foreground window from a new OS
// thread. A new thread starts on the process's desktop, the input desktop.
func onInputDesktop() ([2]int32, uintptr) {
	type res struct {
		c [2]int32
		f uintptr
	}
	ch := make(chan res)
	go func() {
		runtime.LockOSThread()
		ch <- res{cursor(), fg()}
	}()
	r := <-ch
	return r.c, r.f
}

func launch(desk string, argv []string) (*exec.Cmd, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	// exec.Cmd has no desktop field, so start the process with CreateProcess.
	si := &windows.StartupInfo{Desktop: u16(`WinSta0\` + desk)}
	si.Cb = uint32(unsafe.Sizeof(*si))
	pi := &windows.ProcessInformation{}
	line := windows.ComposeCommandLine(argv)
	err := windows.CreateProcess(nil, u16(line), nil, nil, false, 0, nil, nil, si, pi)
	if err != nil {
		return nil, err
	}
	windows.CloseHandle(pi.Thread)
	p, _ := os.FindProcess(int(pi.ProcessId))
	windows.CloseHandle(pi.Process)
	cmd.Process = p
	return cmd, nil
}

func enum(desk uintptr) []uintptr {
	var hs []uintptr
	cb := windows.NewCallback(func(h, _ uintptr) uintptr { hs = append(hs, h); return 1 })
	pEnumDesktopWindows.Call(desk, cb, 0)
	return hs
}

func children(h uintptr) []uintptr {
	var hs []uintptr
	cb := windows.NewCallback(func(c, _ uintptr) uintptr { hs = append(hs, c); return 1 })
	pEnumChildWindows.Call(h, cb, 0)
	return hs
}

// shot captures a window with PrintWindow and returns the image and whether
// PrintWindow reported success.
func shot(h uintptr, w, ht int32) (*image.RGBA, bool) {
	sdc, _, _ := pGetDC.Call(0)
	defer pReleaseDC.Call(0, sdc)
	mdc, _, _ := pCreateCompatibleDC.Call(sdc)
	defer pDeleteDC.Call(mdc)
	type bih struct {
		Size                  uint32
		W, H                  int32
		Planes, Bits          uint16
		Compr, SizeImg        uint32
		XPPM, YPPM            int32
		ClrUsed, ClrImportant uint32
	}
	bi := bih{Size: 40, W: w, H: -ht, Planes: 1, Bits: 32}
	var bits unsafe.Pointer
	bmp, _, _ := pCreateDIBSection.Call(mdc, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 {
		return nil, false
	}
	defer pDeleteObject.Call(bmp)
	pSelectObject.Call(mdc, bmp)
	ok, _, _ := pPrintWindow.Call(h, mdc, pwRenderFullContent)
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(ht)))
	src := unsafe.Slice((*byte)(bits), int(w*ht*4))
	for i := 0; i < len(src); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = src[i+2], src[i+1], src[i], 255
	}
	return img, ok != 0
}

func nonBlack(img *image.RGBA) float64 {
	n := 0
	for i := 0; i < len(img.Pix); i += 4 {
		if img.Pix[i] > 16 || img.Pix[i+1] > 16 || img.Pix[i+2] > 16 {
			n++
		}
	}
	return float64(n) / float64(len(img.Pix)/4)
}

func postText(h uintptr, s string) {
	for _, r := range s {
		pPostMessage.Call(h, wmChar, uintptr(r), 1)
		time.Sleep(30 * time.Millisecond)
	}
}

func getText(h uintptr) string {
	n, _, _ := pSendMessage.Call(h, wmGetTextLength, 0, 0)
	b := make([]uint16, n+1)
	pSendMessage.Call(h, wmGetText, n+1, uintptr(unsafe.Pointer(&b[0])))
	return windows.UTF16ToString(b)
}

const page = `data:text/html,<title>spike-empty</title><body style="background:%23e33;margin:0"><textarea autofocus id=t style="width:90%25;height:200px;font-size:40px" oninput="document.title='spike:'+this.value"></textarea>`

func main() {
	out := flag.String("out", `C:\spike-b`, "output directory")
	gpu := flag.Bool("gpu", true, "let Chrome use the GPU")
	occl := flag.Bool("no-occlusion", false, "turn off Chrome's occlusion and background throttling")
	hold := flag.Duration("hold", 0, "keep the apps running this long before cleanup")
	flag.Parse()
	runtime.LockOSThread()
	os.MkdirAll(*out, 0o755)
	r := report{Desktop: "hc-spike-b", Launched: map[string]string{}, Input: map[string]string{}}
	errf := func(f string, a ...any) { r.Errors = append(r.Errors, fmt.Sprintf(f, a...)) }

	r.CursorBefore, r.FgBefore = onInputDesktop()
	desk, _, e := pCreateDesktop.Call(uintptr(unsafe.Pointer(u16(r.Desktop))), 0, 0, 0, genericAll, 0)
	if desk == 0 {
		errf("CreateDesktop: %v", e)
		write(*out, r)
		return
	}
	defer pCloseDesktop.Call(desk)

	chrome := `C:\Program Files\Google\Chrome\Application\chrome.exe`
	cargs := []string{chrome, "--user-data-dir=" + filepath.Join(*out, "chrome-profile"),
		"--no-first-run", "--no-default-browser-check", "--remote-debugging-port=9301",
		"--window-size=1280,800", "--window-position=0,0", "--new-window", page}
	if *occl {
		cargs = append(cargs[:1], append([]string{"--disable-features=CalculateNativeWinOcclusion",
			"--disable-backgrounding-occluded-windows", "--disable-renderer-backgrounding"}, cargs[1:]...)...)
	}
	if !*gpu {
		cargs = append(cargs[:1], append([]string{"--disable-gpu"}, cargs[1:]...)...)
	}
	apps := map[string][]string{
		"chrome":  cargs,
		"charmap": {`C:\Windows\System32\charmap.exe`},
		"notepad": {`C:\Windows\System32\notepad.exe`},
	}
	var procs []*exec.Cmd
	for name, argv := range apps {
		c, err := launch(r.Desktop, argv)
		if err != nil {
			r.Launched[name] = "error: " + err.Error()
			continue
		}
		r.Launched[name] = fmt.Sprintf("pid %d", c.Process.Pid)
		procs = append(procs, c)
	}
	time.Sleep(10 * time.Second)

	if ok, _, e := pSetThreadDesktop.Call(desk); ok == 0 {
		errf("SetThreadDesktop: %v", e)
	}
	var chromeTop uintptr
	for _, h := range enum(desk) {
		if v, _, _ := pIsWindowVisible.Call(h); v == 0 {
			continue
		}
		var rc rect
		pGetWindowRect.Call(h, uintptr(unsafe.Pointer(&rc)))
		w := win{HWND: h, Title: text(h, pGetWindowText), Class: text(h, pGetClassName), W: rc.R - rc.L, H: rc.B - rc.T}
		pGetWindowThreadPID.Call(h, uintptr(unsafe.Pointer(&w.PID)))
		if w.W >= 100 && w.H >= 100 {
			t := time.Now()
			img, ok := shot(h, w.W, w.H)
			w.ShotMS, w.PrintOK = time.Since(t).Milliseconds(), ok
			if img != nil {
				w.NonBlack = nonBlack(img)
				w.Shot = filepath.Join(*out, fmt.Sprintf("%x.png", h))
				if f, err := os.Create(w.Shot); err == nil {
					png.Encode(f, img)
					f.Close()
				}
			}
		}
		if w.Class == "Chrome_WidgetWin_1" && strings.HasPrefix(w.Title, "spike") {
			chromeTop = h
		}
		r.Windows = append(r.Windows, w)
	}

	// Input through window messages: SendInput only reaches the input desktop.
	if chromeTop != 0 {
		for _, c := range children(chromeTop) {
			r.ChromeKids = append(r.ChromeKids, text(c, pGetClassName))
			if text(c, pGetClassName) == "Chrome_RenderWidgetHostHWND" {
				postText(c, "hi agent")
				time.Sleep(time.Second)
				r.Input["chrome_title_after_wm_char"] = text(chromeTop, pGetWindowText)
				break
			}
		}
	} else {
		r.Input["chrome"] = "no chrome window with a spike title"
	}
	for _, w := range r.Windows {
		for _, c := range children(w.HWND) {
			cls := text(c, pGetClassName)
			if cls == "Edit" || strings.HasPrefix(cls, "RichEdit") {
				postText(c, "hi agent")
				time.Sleep(500 * time.Millisecond)
				r.Input[fmt.Sprintf("%s/%s", w.Class, cls)] = getText(c)
			}
		}
	}

	// UI Automation, read from a helper that runs on the hidden desktop.
	uiaOut := filepath.Join(*out, "uia.txt")
	exe, _ := os.Executable()
	ps := filepath.Join(filepath.Dir(exe), "uia.ps1")
	if c, err := launch(r.Desktop, []string{`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
		"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", ps, uiaOut}); err != nil {
		r.UIA = "launch error: " + err.Error()
	} else {
		for i := 0; i < 60; i++ {
			if _, err := os.Stat(uiaOut + ".done"); err == nil {
				break
			}
			time.Sleep(time.Second)
		}
		_ = c
		b, _ := os.ReadFile(uiaOut)
		r.UIA = string(b)
	}

	r.CursorAfter, r.FgAfter = onInputDesktop()
	write(*out, r)

	time.Sleep(*hold)
	for _, c := range procs {
		exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprint(c.Process.Pid)).Run()
	}
	time.Sleep(2 * time.Second)
	for _, h := range enum(desk) {
		var pid uint32
		pGetWindowThreadPID.Call(h, uintptr(unsafe.Pointer(&pid)))
		r.LeftoverProcs = append(r.LeftoverProcs, fmt.Sprintf("%s pid %d", text(h, pGetClassName), pid))
	}
	write(*out, r)
}

func write(dir string, r report) {
	b, _ := json.MarshalIndent(r, "", "  ")
	os.WriteFile(filepath.Join(dir, "report.json"), b, 0o644)
}
