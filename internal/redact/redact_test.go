package redact

import (
	"strings"
	"testing"
)

func TestTextRemovesPrivateData(t *testing.T) {
	in := strings.Join([]string{
		"open /home/arjun/.config/offstage/config.toml",
		`C:\Users\aswin\AppData\Local\offstage\log`,
		"/Users/joel/Library",
		"https://app.example.com/login?session=abc123def#tok",
		"ws://127.0.0.1:9222/devtools/page/1?wstoken=zz99",
		"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.payload",
		"key plane_api_0123456789abcdef and osk_ABCDEFGHIJKLMNOP",
		`{"password": "hunter22secret"}`,
		"mail arjun@example.com",
	}, "\n")
	out := Text(in)
	for _, bad := range []string{"arjun", "aswin", "joel", "session=", "abc123def", "eyJhbGci", "0123456789abcdef", "ABCDEFGHIJKLMNOP", "hunter22secret", "@example.com", "wstoken"} {
		if strings.Contains(out, bad) {
			t.Errorf("output still has %q:\n%s", bad, out)
		}
	}
	for _, keep := range []string{"~/.config/offstage/config.toml", "https://app.example.com/login", "~\\AppData"} {
		if !strings.Contains(out, keep) {
			t.Errorf("output lost %q:\n%s", keep, out)
		}
	}
}
