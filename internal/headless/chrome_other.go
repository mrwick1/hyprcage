//go:build !windows

package headless

// installedChrome has no standard install folder to look in.
func installedChrome() string { return "" }
