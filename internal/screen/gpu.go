package screen

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hexadecimil/offstage/internal/wl"
)

// RenderDevice picks the DRM render node cage renders on, and says how it
// was picked. cage on its headless backend has no compositor above it to
// borrow a GPU from, so the choice is made here, in this order:
//
//  1. cage.render_device in the configuration;
//  2. the GPU the human's compositor was pinned to, when its environment
//     says so (WLR_RENDER_DRM_DEVICE, AQ_DRM_DEVICES for Hyprland,
//     WLR_DRM_DEVICES for wlroots compositors);
//  3. the GPU the human's compositor renders with, as it tells every client
//     through the linux-dmabuf feedback (main_device);
//  4. the first render node of the machine.
//
// Staying on the human's GPU matters on a laptop with two of them: the
// other one would be woken up for nothing, and its frames would have to
// cross over for the mirror.
func RenderDevice(configured, display string) (path, how string) {
	if configured != "" {
		if p := renderNodeOf(configured); p != "" {
			return p, "cage.render_device"
		}
	}
	for _, v := range []string{"WLR_RENDER_DRM_DEVICE", "AQ_DRM_DEVICES", "WLR_DRM_DEVICES"} {
		for _, cand := range strings.Split(os.Getenv(v), ":") {
			if cand == "" {
				continue
			}
			if p := renderNodeOf(cand); p != "" {
				return p, v
			}
		}
	}
	if dev, err := wl.MainDevice(display); err == nil && dev != 0 {
		if p := renderNodeOfDev(dev); p != "" {
			return p, "linux-dmabuf main_device"
		}
	}
	nodes, _ := filepath.Glob("/dev/dri/renderD*")
	sort.Strings(nodes)
	if len(nodes) > 0 {
		return nodes[0], "first render node"
	}
	return "", "none"
}

// renderNodeOf maps a DRM node path (card, render, or a udev symlink to
// either) to the render node of the same device.
func renderNodeOf(p string) string {
	fi, err := os.Stat(p)
	if err != nil {
		return ""
	}
	rdev, ok := deviceNumber(fi)
	if !ok || fi.Mode()&os.ModeCharDevice == 0 {
		return ""
	}
	return renderNodeOfDev(rdev)
}

// renderNodeOfDev finds the render node of the DRM device a character
// device number belongs to, through sysfs: every node of a device is
// listed under its PCI (or platform) device's drm directory.
func renderNodeOfDev(dev uint64) string {
	major, minor := unixMajor(dev), unixMinor(dev)
	entries, err := os.ReadDir(fmt.Sprintf("/sys/dev/char/%d:%d/device/drm", major, minor))
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "renderD") {
			p := "/dev/dri/" + e.Name()
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

func unixMajor(dev uint64) uint32 {
	return uint32(((dev >> 32) & 0xfffff000) | ((dev >> 8) & 0xfff))
}

func unixMinor(dev uint64) uint32 {
	return uint32(((dev >> 12) & 0xffffff00) | (dev & 0xff))
}
