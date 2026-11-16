//go:build !windows

package headless

// installedChrome has no standard install folder to look in.
func installedChrome() string { return "" }

// seedProfile has nothing to write: only Chrome on Windows tests the password.
func seedProfile(dir string) error { return nil }
