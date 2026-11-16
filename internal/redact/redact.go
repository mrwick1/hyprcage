// Package redact removes private data from report text before it leaves a
// machine. The offstage client runs it on a bundle, and the beta relay runs
// it again on what it receives.
package redact

import "regexp"

var rules = []struct {
	re   *regexp.Regexp
	repl string
}{
	// Tokens and keys with a known prefix, and bearer headers.
	{regexp.MustCompile(`\b(plane_api_|osk_|ghp_|gho_|github_pat_|sk-|xox[abp]-)[A-Za-z0-9_\-]{8,}`), "<token>"},
	{regexp.MustCompile(`(?i)\b(bearer|token|api[_-]?key|password|passwd|secret|cookie)(["']?\s*[:=]\s*["']?|\s+)[^\s"',;&]{6,}`), "$1$2<redacted>"},
	// Query strings and fragments of URLs.
	{regexp.MustCompile(`\b([a-z][a-z0-9+.\-]*://[^\s?#"'<>]+)[?#][^\s"'<>]*`), "$1"},
	// Home directories on Linux, macOS and Windows.
	{regexp.MustCompile(`/home/[^/\s"']+`), "~"},
	{regexp.MustCompile(`/Users/[^/\s"']+`), "~"},
	{regexp.MustCompile(`(?i)[A-Z]:\\Users\\[^\\\s"']+`), "~"},
	// Email addresses.
	{regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`), "<email>"},
}

// Text returns s with tokens, URL queries, home directories and email
// addresses replaced.
func Text(s string) string {
	for _, r := range rules {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}
