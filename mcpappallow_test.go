package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Step 2 phase 3: "Let the page run its tools?" answers, remembered on this
// machine across sessions. After a Yes in an earlier session, the question
// also offers Always allow.

func useMcpAppSession(t *testing.T, id string) {
	t.Helper()
	orig := mcpAppSessionID
	mcpAppSessionID = id
	t.Cleanup(func() { mcpAppSessionID = orig })
}

func allowReq(t *testing.T, method, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/api/mcp-app/allow"
	if method == http.MethodGet {
		target += "?server=" + body
		body = ""
	}
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	handleMcpAppAllow(rec, req)
	return rec
}

func allowState(t *testing.T, server string) mcpAppAllowState {
	t.Helper()
	rec := allowReq(t, http.MethodGet, server, nil)
	if rec.Code != 200 {
		t.Fatalf("GET allow: %d %s", rec.Code, rec.Body.String())
	}
	var st mcpAppAllowState
	json.Unmarshal(rec.Body.Bytes(), &st)
	return st
}

func answerAllow(t *testing.T, server, answer string) {
	t.Helper()
	if rec := allowReq(t, http.MethodPost, `{"server":"`+server+`","answer":"`+answer+`"}`, nil); rec.Code != 200 {
		t.Fatalf("POST %s: %d %s", answer, rec.Code, rec.Body.String())
	}
}

func TestMcpAppAllowOffersAlwaysAfterYesInAnotherSession(t *testing.T) {
	base := useMcpAppBase(t)
	useMcpAppSession(t, "s1")
	if st := allowState(t, "cal"); st.Always || st.OfferAlways {
		t.Fatalf("fresh: %+v", st)
	}
	answerAllow(t, "cal", "yes")
	answerAllow(t, "cal", "yes") // same session twice counts once
	if st := allowState(t, "cal"); st.OfferAlways {
		t.Errorf("a Yes in this same session must not offer Always allow yet: %+v", st)
	}

	useMcpAppSession(t, "s2")
	if st := allowState(t, "cal"); !st.OfferAlways || st.Always {
		t.Errorf("second session: %+v, want offerAlways", st)
	}
	if st := allowState(t, "other"); st.OfferAlways {
		t.Errorf("another tool program must not inherit cal's answers: %+v", st)
	}
	answerAllow(t, "cal", "always")
	if st := allowState(t, "cal"); !st.Always {
		t.Errorf("after always: %+v", st)
	}

	fi, err := os.Stat(filepath.Join(base, "mcp-app-allow.json"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("allow file: %v, mode %v", err, fi)
	}
}

func TestMcpAppAllowNoResetsAndForgetClears(t *testing.T) {
	useMcpAppBase(t)
	useMcpAppSession(t, "s1")
	answerAllow(t, "cal", "yes")
	useMcpAppSession(t, "s2")
	answerAllow(t, "cal", "no")
	if st := allowState(t, "cal"); st.OfferAlways || st.Always {
		t.Errorf("a No resets the count: %+v", st)
	}
	answerAllow(t, "cal", "always")
	if rec := allowReq(t, http.MethodDelete, "", nil); rec.Code != 200 {
		t.Fatalf("DELETE: %d", rec.Code)
	}
	if st := allowState(t, "cal"); st.Always || st.OfferAlways {
		t.Errorf("after forget: %+v", st)
	}
}

func TestMcpAppAllowOnlyFromTheChatPage(t *testing.T) {
	useMcpAppBase(t)
	if rec := allowReq(t, http.MethodPost, `{"server":"cal","answer":"always"}`, map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site POST: %d", rec.Code)
	}
	if rec := allowReq(t, http.MethodPost, `{"server":"cal","answer":"always"}`, map[string]string{"Content-Type": "text/plain"}); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain POST: %d", rec.Code)
	}
	if rec := allowReq(t, http.MethodDelete, "", map[string]string{"Sec-Fetch-Site": "same-site"}); rec.Code != http.StatusForbidden {
		t.Errorf("same-site DELETE: %d", rec.Code)
	}
	if rec := allowReq(t, http.MethodPost, `{"server":"cal","answer":"maybe"}`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad answer: %d", rec.Code)
	}
	if rec := allowReq(t, http.MethodPost, `{"server":"","answer":"yes"}`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("no server: %d", rec.Code)
	}
	if st := allowState(t, "cal"); st.Always {
		t.Errorf("a refused request changed the store: %+v", st)
	}
}
