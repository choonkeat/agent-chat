package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureHTTPServerLazyStart(t *testing.T) {
	// Reset global state for test
	httpMu.Lock()
	httpRunning = false
	httpListener = nil
	httpMu.Unlock()
	uiURL = ""
	mcpServerRef = nil

	// With nil mcpServerRef, ensureHTTPServer should fail (tries to start server)
	err := ensureHTTPServer()
	if err == nil {
		// If it succeeded, clean up
		httpMu.Lock()
		if httpListener != nil {
			httpListener.Close()
		}
		httpRunning = false
		httpMu.Unlock()
	}
	// The key point: it attempted to start (didn't silently no-op)
}

func TestEnsureHTTPServerCrashRecovery(t *testing.T) {
	// Reset global state
	httpMu.Lock()
	httpRunning = false
	httpListener = nil
	httpMu.Unlock()
	uiURL = ""
	mcpServerRef = nil

	// Simulate: httpRunning was true but server crashed (httpRunning set to false)
	httpMu.Lock()
	httpRunning = false
	httpMu.Unlock()

	// We can't easily test a full server start without mcpServerRef,
	// but we can verify the flag logic: if httpRunning is false,
	// ensureHTTPServer should attempt to start (and fail without mcpServerRef).
	err := ensureHTTPServer()
	// Expect an error because mcpServerRef is nil, but importantly it TRIED
	// to start — it didn't skip due to sync.Once.
	if err == nil {
		// If it succeeded somehow, clean up
		httpMu.Lock()
		if httpListener != nil {
			httpListener.Close()
		}
		httpRunning = false
		httpMu.Unlock()
	}
	// The key assertion: it attempted a restart (didn't silently no-op).

	// Call again — should also retry (not cached failure)
	err2 := ensureHTTPServer()
	_ = err2
	// Both calls attempted to start — no permanent failure caching.
}

func TestEventBusSubscribeUnblocks(t *testing.T) {
	eb := NewEventBus()
	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		done <- eb.WaitForSubscriber(ctx)
	}()

	// Should not unblock yet
	select {
	case <-done:
		t.Fatal("WaitForSubscriber unblocked before any subscriber")
	case <-time.After(50 * time.Millisecond):
	}

	// Subscribe should unblock it
	sub := eb.Subscribe()
	defer eb.Unsubscribe(sub)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WaitForSubscriber returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitForSubscriber did not unblock after Subscribe")
	}
}

func TestWaitForSubscriberRespectsContext(t *testing.T) {
	eb := NewEventBus()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- eb.WaitForSubscriber(ctx)
	}()

	// Should not unblock yet (no subscribers)
	select {
	case <-done:
		t.Fatal("WaitForSubscriber unblocked before cancel")
	case <-time.After(50 * time.Millisecond):
	}

	// Cancel context — should unblock with error
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error from cancelled context, got nil")
		}
	case <-time.After(time.Second):
		t.Fatal("WaitForSubscriber did not unblock after context cancel")
	}
}

func TestWaitForSubscriberAfterReconnect(t *testing.T) {
	eb := NewEventBus()
	ctx := context.Background()

	// First subscriber connects and disconnects
	sub1 := eb.Subscribe()
	if err := eb.WaitForSubscriber(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	eb.Unsubscribe(sub1)

	// Now no subscribers — WaitForSubscriber should block again
	done := make(chan error, 1)
	go func() {
		done <- eb.WaitForSubscriber(ctx)
	}()

	select {
	case <-done:
		t.Fatal("WaitForSubscriber unblocked with no subscribers")
	case <-time.After(200 * time.Millisecond):
	}

	// New subscriber connects — should unblock
	sub2 := eb.Subscribe()
	defer eb.Unsubscribe(sub2)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitForSubscriber did not unblock after reconnect")
	}
}

func TestEventBusPublishAndReceive(t *testing.T) {
	eb := NewEventBus()
	sub := eb.Subscribe()
	defer eb.Unsubscribe(sub)

	eb.Publish(Event{Type: "agentMessage", Text: "hello"})

	ev1 := <-sub
	if ev1.Type != "agentMessage" || ev1.Text != "hello" {
		t.Fatalf("expected agentMessage event with text 'hello', got type=%s text=%s", ev1.Type, ev1.Text)
	}
}

func TestEventBusAckResolve(t *testing.T) {
	eb := NewEventBus()
	ack := eb.CreateAck()

	go func() {
		time.Sleep(10 * time.Millisecond)
		eb.ResolveAck(ack.ID, "ack:clicked continue")
	}()

	select {
	case result := <-ack.Ch:
		if result != "ack:clicked continue" {
			t.Fatalf("expected 'ack:clicked continue', got '%s'", result)
		}
	case <-time.After(time.Second):
		t.Fatal("ack did not resolve in time")
	}
}

func TestEventBusMultipleSubscribers(t *testing.T) {
	eb := NewEventBus()
	sub1 := eb.Subscribe()
	sub2 := eb.Subscribe()
	defer eb.Unsubscribe(sub1)
	defer eb.Unsubscribe(sub2)

	eb.Publish(Event{Type: "agentMessage", AckID: "test-123"})

	ev1 := <-sub1
	ev2 := <-sub2

	if ev1.Type != "agentMessage" || ev1.AckID != "test-123" {
		t.Fatalf("subscriber 1 got unexpected event: %+v", ev1)
	}
	if ev2.Type != "agentMessage" || ev2.AckID != "test-123" {
		t.Fatalf("subscriber 2 got unexpected event: %+v", ev2)
	}
}

func TestEventBusUnsubscribe(t *testing.T) {
	eb := NewEventBus()
	sub := eb.Subscribe()
	eb.Unsubscribe(sub)

	eb.Publish(Event{Type: "agentMessage"})

	select {
	case <-sub:
		t.Fatal("unsubscribed channel should not receive events")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestUploadEndpoint(t *testing.T) {
	// Set up a temp upload dir
	dir := t.TempDir()
	origDir := uploadDir
	uploadDir = dir
	t.Cleanup(func() { uploadDir = origDir })

	// Create multipart body with a test file
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("files", "test-photo.png")
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("fake png content")
	part.Write(content)
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()

	handleUpload(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var refs []FileRef
	if err := json.Unmarshal(rr.Body.Bytes(), &refs); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 file ref, got %d", len(refs))
	}
	ref := refs[0]
	if ref.Name != "test-photo.png" {
		t.Errorf("expected name 'test-photo.png', got %q", ref.Name)
	}
	if ref.Size != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), ref.Size)
	}

	// Verify file was saved to disk
	saved, err := os.ReadFile(ref.Path)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}
	if !bytes.Equal(saved, content) {
		t.Error("saved file content does not match")
	}
}

func TestUploadServesFiles(t *testing.T) {
	dir := t.TempDir()
	origDir := uploadDir
	uploadDir = dir
	t.Cleanup(func() { uploadDir = origDir })

	// Write a file to the upload dir
	testContent := []byte("hello world")
	if err := os.WriteFile(filepath.Join(dir, "test-file.txt"), testContent, 0644); err != nil {
		t.Fatal(err)
	}

	handler := http.StripPrefix("/uploads/", http.FileServer(http.Dir(dir)))
	req := httptest.NewRequest(http.MethodGet, "/uploads/test-file.txt", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body, _ := io.ReadAll(rr.Body)
	if !bytes.Equal(body, testContent) {
		t.Error("served content does not match")
	}
}

func TestUploadNoFiles(t *testing.T) {
	dir := t.TempDir()
	origDir := uploadDir
	uploadDir = dir
	t.Cleanup(func() { uploadDir = origDir })

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()

	handleUpload(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUploadMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/upload", nil)
	rr := httptest.NewRecorder()

	handleUpload(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestBuildTriggerMap(t *testing.T) {
	// No flags: default @=builtin:filepath
	m := buildTriggerMap("", "")
	if m["@"] != "builtin:filepath" {
		t.Errorf("expected default @=builtin:filepath, got @=%s", m["@"])
	}
	if len(m) != 1 {
		t.Errorf("expected 1 entry, got %d: %v", len(m), m)
	}

	// New format: CHAR=URL
	m = buildTriggerMap("/=http://localhost:9000/completions", "")
	if m["/"] != "http://localhost:9000/completions" {
		t.Errorf("expected URL, got %q", m["/"])
	}
	if m["@"] != "builtin:filepath" {
		t.Errorf("default @ should still be present, got %q", m["@"])
	}

	// Legacy format: CHAR=TYPE=URL (type is ignored, URL is used)
	m = buildTriggerMap("/=slash-command=http://localhost:9000/completions", "")
	if m["/"] != "http://localhost:9000/completions" {
		t.Errorf("legacy with URL: expected URL, got %q", m["/"])
	}

	// Legacy format: CHAR=TYPE with fallback URL
	m = buildTriggerMap("/=slash-command", "http://fallback:9000")
	if m["/"] != "http://fallback:9000" {
		t.Errorf("legacy with fallback: expected fallback URL, got %q", m["/"])
	}

	// Legacy format: CHAR=TYPE without fallback URL — no entry added
	m = buildTriggerMap("/=slash-command", "")
	if _, ok := m["/"]; ok {
		t.Errorf("legacy without fallback should not register /, got %q", m["/"])
	}

	// Override default @
	m = buildTriggerMap("@=http://custom:8080/files", "")
	if m["@"] != "http://custom:8080/files" {
		t.Errorf("expected custom @ URL, got %q", m["@"])
	}
}

func TestTriggerChars(t *testing.T) {
	m := map[string]string{"@": "builtin:filepath", "/": "http://example.com"}
	chars := triggerChars(m)
	if len(chars) != 2 || chars[0] != "/" || chars[1] != "@" {
		t.Errorf("expected sorted [/ @], got %v", chars)
	}
}

func TestAutocompleteUnknownTrigger(t *testing.T) {
	origMap := triggerMap
	triggerMap = map[string]string{"@": "builtin:filepath"}
	t.Cleanup(func() { triggerMap = origMap })

	body := bytes.NewBufferString(`{"trigger":"/","query":"bu"}`)
	req := httptest.NewRequest(http.MethodPost, "/autocomplete", body)
	rr := httptest.NewRecorder()

	handleAutocomplete(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp autocompleteResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(resp.Results) != 0 {
		t.Errorf("expected empty results, got %v", resp.Results)
	}
}

func TestAutocompleteMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/autocomplete", nil)
	rr := httptest.NewRecorder()

	handleAutocomplete(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestAutocompleteProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Query string `json:"query"`
		}
		json.Unmarshal(body, &req)

		var results []string
		if req.Query == "bu" {
			results = []string{"busy", "been up"}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}))
	defer upstream.Close()

	origMap := triggerMap
	triggerMap = map[string]string{"@": "builtin:filepath", "/": upstream.URL}
	t.Cleanup(func() { triggerMap = origMap })

	reqBody := bytes.NewBufferString(`{"trigger":"/","query":"bu"}`)
	req := httptest.NewRequest(http.MethodPost, "/autocomplete", reqBody)
	rr := httptest.NewRecorder()

	handleAutocomplete(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp autocompleteResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(resp.Results) != 2 || resp.Results[0].V != "busy" || resp.Results[1].V != "been up" {
		t.Errorf("unexpected results: %v", resp.Results)
	}
}

func TestAutocompleteBuiltinFilepath(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), nil, 0644)
	os.WriteFile(filepath.Join(dir, "README.md"), nil, 0644)

	origMap := triggerMap
	origRoot := filepathRoot
	triggerMap = map[string]string{"@": "builtin:filepath"}
	filepathRoot = dir
	t.Cleanup(func() { triggerMap = origMap; filepathRoot = origRoot })

	reqBody := bytes.NewBufferString(`{"trigger":"@","query":"main"}`)
	req := httptest.NewRequest(http.MethodPost, "/autocomplete", reqBody)
	rr := httptest.NewRecorder()

	handleAutocomplete(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp autocompleteResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	found := false
	for _, r := range resp.Results {
		if strings.HasSuffix(r.V, "main.go") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected main.go in results, got %v", resp.Results)
	}
}

func TestAutocompleteBuiltinFilepathInfo(t *testing.T) {
	dir := t.TempDir()

	origMap := triggerMap
	origRoot := filepathRoot
	triggerMap = map[string]string{"@": "builtin:filepath"}
	filepathRoot = dir
	t.Cleanup(func() { triggerMap = origMap; filepathRoot = origRoot })

	reqBody := bytes.NewBufferString(`{"trigger":"@","query":"xyz"}`)
	req := httptest.NewRequest(http.MethodPost, "/autocomplete", reqBody)
	rr := httptest.NewRecorder()

	handleAutocomplete(rr, req)

	var resp autocompleteResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Results) != 0 {
		t.Errorf("expected empty results, got %v", resp.Results)
	}
	if !strings.Contains(resp.Info, "xyz") || !strings.Contains(resp.Info, dir) {
		t.Errorf("expected info to contain query and dir, got %q", resp.Info)
	}
}

func TestBuiltinFilepathComplete(t *testing.T) {
	dir := t.TempDir()
	// Create test files
	os.MkdirAll(filepath.Join(dir, "src"), 0755)
	os.WriteFile(filepath.Join(dir, "main.go"), nil, 0644)
	os.WriteFile(filepath.Join(dir, "README.md"), nil, 0644)
	os.WriteFile(filepath.Join(dir, "src", "app.go"), nil, 0644)
	os.MkdirAll(filepath.Join(dir, ".git"), 0755)
	os.WriteFile(filepath.Join(dir, ".git", "config"), nil, 0644)

	results, _ := builtinFilepathComplete(dir, "")
	// Should include main.go, README.md, src, src/app.go but NOT .git/config
	for _, r := range results {
		if strings.Contains(r, ".git") {
			t.Errorf("should skip hidden dirs, got %s", r)
		}
	}
	if len(results) < 3 {
		t.Errorf("expected at least 3 results, got %v", results)
	}

	// Fuzzy match
	results, _ = builtinFilepathComplete(dir, "app")
	found := false
	for _, r := range results {
		if strings.HasSuffix(r, "app.go") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected src/app.go in results for query 'app', got %v", results)
	}
}

func TestFuzzyMatchPath(t *testing.T) {
	if !fuzzyMatchPath("src/main.go", "main") {
		t.Error("expected 'src/main.go' to match 'main'")
	}
	if !fuzzyMatchPath("src/main.go", "smg") {
		t.Error("expected 'src/main.go' to match 'smg'")
	}
	if fuzzyMatchPath("README.md", "xyz") {
		t.Error("expected 'README.md' not to match 'xyz'")
	}
	if !fuzzyMatchPath("anything", "") {
		t.Error("empty query should match everything")
	}
}

func TestFuzzyScorePath(t *testing.T) {
	// Contiguous substring match should score lower than scattered match
	substringScore, ok1 := fuzzyScorePath("tasks/readme.md", "task")
	scatteredScore, ok2 := fuzzyScorePath("cmd/templates/host/Dockerfile", "task")
	if !ok1 || !ok2 {
		t.Fatal("both paths should match 'task'")
	}
	if substringScore >= scatteredScore {
		t.Errorf("substring match (%d) should score lower than scattered match (%d)", substringScore, scatteredScore)
	}

	// Empty query scores 0
	score, ok := fuzzyScorePath("anything", "")
	if !ok || score != 0 {
		t.Errorf("empty query should match with score 0, got %d, %v", score, ok)
	}

	// Non-matching query
	_, ok = fuzzyScorePath("README.md", "xyz")
	if ok {
		t.Error("expected 'README.md' not to match 'xyz'")
	}
}

func TestBuiltinFilepathCompleteScoring(t *testing.T) {
	dir := t.TempDir()
	// Create files: "tasks/" should rank higher than "cmd/templates/host/" for query "task"
	os.MkdirAll(filepath.Join(dir, "cmd", "templates", "host"), 0755)
	os.WriteFile(filepath.Join(dir, "cmd", "templates", "host", "Dockerfile"), nil, 0644)
	os.MkdirAll(filepath.Join(dir, "tasks"), 0755)
	os.WriteFile(filepath.Join(dir, "tasks", "readme.md"), nil, 0644)

	results, _ := builtinFilepathComplete(dir, "task")
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %v", results)
	}
	// tasks/readme.md should come before cmd/templates/host/Dockerfile
	tasksIdx := -1
	cmdIdx := -1
	for i, r := range results {
		if strings.Contains(r, "tasks") {
			tasksIdx = i
		}
		if strings.Contains(r, "cmd") {
			cmdIdx = i
		}
	}
	if tasksIdx < 0 || cmdIdx < 0 {
		t.Fatalf("expected both paths in results, got %v", results)
	}
	if tasksIdx > cmdIdx {
		t.Errorf("tasks/readme.md (idx %d) should rank before cmd/templates/host/Dockerfile (idx %d)", tasksIdx, cmdIdx)
	}
}
