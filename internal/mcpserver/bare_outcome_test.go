package mcpserver

import "testing"

func TestBareReportFailureIsToolError(t *testing.T) {
	for _, value := range []string{"FAILED not_found: no element matches text=Ready", `{"ok":false,"failure":{"kind":"not_found"}}`} {
		r := plain(value)
		normalizeBareResult("step", r)
		if !r.IsError {
			t.Fatal("native failure reported as success", value)
		}
	}
	for _, tc := range []struct{ name, text string }{{"snapshot", "FAILED is page text"}, {"network", "GET /submit 500"}, {"step", "ok (2 ms)"}, {"step", `{"ok":true}`}} {
		r := plain(tc.text)
		normalizeBareResult(tc.name, r)
		if r.IsError {
			t.Fatal("non-failure misclassified", tc)
		}
	}
}
