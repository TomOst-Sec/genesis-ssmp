package god

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Mock Heaven for Verifier
// ---------------------------------------------------------------------------

func startVerifierHeaven(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("POST /blob", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		// Compute a fake blob ID from content length
		id := hashString(string(body))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"blob_id": id})
	})

	mux.HandleFunc("POST /event", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"offset": 1})
	})

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// mockRunCmd creates a test command runner that returns fixed output.
func mockRunCmd(stdout string, exitCode int) func(string, string) (string, int, error) {
	return func(dir, command string) (string, int, error) {
		return stdout, exitCode, nil
	}
}

// ---------------------------------------------------------------------------
// Receipt creation tests
// ---------------------------------------------------------------------------

func TestVerifyPassCreatesReceipt(t *testing.T) {
	ts := startVerifierHeaven(t)
	client := NewHeavenClient(ts.URL)
	v := NewVerifier(client)
	v.runCmd = mockRunCmd("PASS\nok  \tpkg\t0.005s\n", 0)

	result, err := v.Verify(VerifyRequest{
		MissionID: "m1",
		RepoRoot:  t.TempDir(),
		Command:   "go test ./...",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Passed {
		t.Error("expected pass")
	}
	if result.Receipt.MissionID != "m1" {
		t.Errorf("MissionID = %q", result.Receipt.MissionID)
	}
	if result.Receipt.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.Receipt.ExitCode)
	}
	if result.Receipt.EnvHash == "" {
		t.Error("EnvHash should not be empty")
	}
	if result.Receipt.CommandHash == "" {
		t.Error("CommandHash should not be empty")
	}
	if result.Receipt.StdoutHash == "" {
		t.Error("StdoutHash should not be empty")
	}
	if result.Receipt.Timestamp == "" {
		t.Error("Timestamp should not be empty")
	}
	if _, err := time.Parse(time.RFC3339, result.Receipt.Timestamp); err != nil {
		t.Errorf("Timestamp not valid RFC3339: %v", err)
	}
	if result.BlobID == "" {
		t.Error("BlobID should not be empty (stored in Heaven)")
	}
}

func TestVerifyFailCreatesReceipt(t *testing.T) {
	ts := startVerifierHeaven(t)
	client := NewHeavenClient(ts.URL)
	v := NewVerifier(client)
	v.runCmd = mockRunCmd("FAIL\n--- FAIL: TestSomething\n", 1)

	result, err := v.Verify(VerifyRequest{
		MissionID: "m1",
		RepoRoot:  t.TempDir(),
		Command:   "go test ./...",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Passed {
		t.Error("expected failure")
	}
	if result.Receipt.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", result.Receipt.ExitCode)
	}
	if result.BlobID == "" {
		t.Error("BlobID should still be stored even on failure")
	}
}

func TestVerifyReceiptCommandHash(t *testing.T) {
	ts := startVerifierHeaven(t)
	client := NewHeavenClient(ts.URL)
	v := NewVerifier(client)
	v.runCmd = mockRunCmd("ok", 0)

	result1, _ := v.Verify(VerifyRequest{MissionID: "m1", RepoRoot: t.TempDir(), Command: "go test ./..."})
	result2, _ := v.Verify(VerifyRequest{MissionID: "m1", RepoRoot: t.TempDir(), Command: "npm test"})

	if result1.Receipt.CommandHash == result2.Receipt.CommandHash {
		t.Error("different commands should have different hashes")
	}
}

func TestVerifyReceiptStdoutHash(t *testing.T) {
	ts := startVerifierHeaven(t)
	client := NewHeavenClient(ts.URL)
	v := NewVerifier(client)

	v.runCmd = mockRunCmd("output A", 0)
	result1, _ := v.Verify(VerifyRequest{MissionID: "m1", RepoRoot: t.TempDir(), Command: "test"})

	v.runCmd = mockRunCmd("output B", 0)
	result2, _ := v.Verify(VerifyRequest{MissionID: "m1", RepoRoot: t.TempDir(), Command: "test"})

	if result1.Receipt.StdoutHash == result2.Receipt.StdoutHash {
		t.Error("different stdout should produce different hashes")
	}
}

func TestVerifyStdoutCaptured(t *testing.T) {
	ts := startVerifierHeaven(t)
	client := NewHeavenClient(ts.URL)
	v := NewVerifier(client)
	v.runCmd = mockRunCmd("test output here\n", 0)

	result, _ := v.Verify(VerifyRequest{MissionID: "m1", RepoRoot: t.TempDir(), Command: "test"})
	if result.Stdout != "test output here\n" {
		t.Errorf("Stdout = %q", result.Stdout)
	}
}

// ---------------------------------------------------------------------------
// DetectTestCommand tests
// ---------------------------------------------------------------------------

func TestDetectTestCommandGo(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0o644)
	cmd := DetectTestCommand(dir)
	if cmd != "go test ./..." {
		t.Errorf("got %q, want 'go test ./...'", cmd)
	}
}

func TestDetectTestCommandNode(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644)
	cmd := DetectTestCommand(dir)
	if cmd != "npm test" {
		t.Errorf("got %q, want 'npm test'", cmd)
	}
}

func TestDetectTestCommandPython(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[project]\n"), 0o644)
	cmd := DetectTestCommand(dir)
	if cmd != "python -m pytest" {
		t.Errorf("got %q, want 'python -m pytest'", cmd)
	}
}

func TestDetectTestCommandRust(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\n"), 0o644)
	cmd := DetectTestCommand(dir)
	if cmd != "cargo test" {
		t.Errorf("got %q, want 'cargo test'", cmd)
	}
}

func TestDetectTestCommandFallback(t *testing.T) {
	dir := t.TempDir()
	cmd := DetectTestCommand(dir)
	if !strings.Contains(cmd, "echo") {
		t.Errorf("fallback should use echo, got %q", cmd)
	}
}

// ---------------------------------------------------------------------------
// ComputeEnvHash tests
// ---------------------------------------------------------------------------

func TestComputeEnvHashEmpty(t *testing.T) {
	dir := t.TempDir()
	h := ComputeEnvHash(dir)
	if h == "" {
		t.Error("should produce a hash even with no lockfiles")
	}
	if len(h) != 64 {
		t.Errorf("hash length = %d, want 64 (SHA256 hex)", len(h))
	}
}

func TestComputeEnvHashWithLockfile(t *testing.T) {
	dir := t.TempDir()
	h1 := ComputeEnvHash(dir)

	// Add a lockfile
	os.WriteFile(filepath.Join(dir, "go.sum"), []byte("module test v1.0.0\n"), 0o644)
	h2 := ComputeEnvHash(dir)

	if h1 == h2 {
		t.Error("adding a lockfile should change the hash")
	}
}

func TestComputeEnvHashDeterministic(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.sum"), []byte("content"), 0o644)

	h1 := ComputeEnvHash(dir)
	h2 := ComputeEnvHash(dir)
	if h1 != h2 {
		t.Error("same inputs should produce same hash")
	}
}

func TestComputeEnvHashDifferentContent(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	os.WriteFile(filepath.Join(dir1, "go.sum"), []byte("v1"), 0o644)
	os.WriteFile(filepath.Join(dir2, "go.sum"), []byte("v2"), 0o644)

	h1 := ComputeEnvHash(dir1)
	h2 := ComputeEnvHash(dir2)
	if h1 == h2 {
		t.Error("different lockfile contents should produce different hashes")
	}
}

// ---------------------------------------------------------------------------
// GateMerge tests
// ---------------------------------------------------------------------------

func TestGateMergePass(t *testing.T) {
	receipt := Receipt{
		MissionID:   "m1",
		EnvHash:     "abc",
		CommandHash: "def",
		StdoutHash:  "ghi",
		ExitCode:    0,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	if err := GateMerge(receipt); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestGateMergeFailExitCode(t *testing.T) {
	receipt := Receipt{
		MissionID:   "m1",
		EnvHash:     "abc",
		CommandHash: "def",
		StdoutHash:  "ghi",
		ExitCode:    1,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	err := GateMerge(receipt)
	if err == nil {
		t.Fatal("expected error for non-zero exit code")
	}
	if !strings.Contains(err.Error(), "exit code 1") {
		t.Errorf("error should mention exit code: %v", err)
	}
}

func TestGateMergeMissingMissionID(t *testing.T) {
	receipt := Receipt{
		ExitCode:  0,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	err := GateMerge(receipt)
	if err == nil {
		t.Fatal("expected error for missing mission_id")
	}
}

func TestGateMergeMissingTimestamp(t *testing.T) {
	receipt := Receipt{
		MissionID: "m1",
		ExitCode:  0,
	}
	err := GateMerge(receipt)
	if err == nil {
		t.Fatal("expected error for missing timestamp")
	}
}

func TestGateMergeBadTimestamp(t *testing.T) {
	receipt := Receipt{
		MissionID: "m1",
		ExitCode:  0,
		Timestamp: "not-a-timestamp",
	}
	err := GateMerge(receipt)
	if err == nil {
		t.Fatal("expected error for invalid timestamp")
	}
	if !strings.Contains(err.Error(), "invalid timestamp") {
		t.Errorf("error = %v", err)
	}
}

// ---------------------------------------------------------------------------
// Receipt JSON schema conformance
// ---------------------------------------------------------------------------

func TestReceiptJSONSchema(t *testing.T) {
	ts := startVerifierHeaven(t)
	client := NewHeavenClient(ts.URL)
	v := NewVerifier(client)
	v.runCmd = mockRunCmd("ok", 0)

	result, err := v.Verify(VerifyRequest{MissionID: "m1", RepoRoot: t.TempDir(), Command: "test"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	data, err := json.Marshal(result.Receipt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify all required fields from proto/receipts.schema.json are present
	var m map[string]any
	json.Unmarshal(data, &m)

	required := []string{"mission_id", "env_hash", "command_hash", "stdout_hash", "exit_code", "timestamp"}
	for _, field := range required {
		if _, ok := m[field]; !ok {
			t.Errorf("receipt JSON missing required field %q", field)
		}
	}
}

// ---------------------------------------------------------------------------
// Integration: Verify → GateMerge
// ---------------------------------------------------------------------------

func TestVerifyThenGatePass(t *testing.T) {
	ts := startVerifierHeaven(t)
	client := NewHeavenClient(ts.URL)
	v := NewVerifier(client)
	v.runCmd = mockRunCmd("all tests passed", 0)

	result, err := v.Verify(VerifyRequest{MissionID: "m1", RepoRoot: t.TempDir(), Command: "test"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	if err := GateMerge(result.Receipt); err != nil {
		t.Fatalf("gate should pass: %v", err)
	}
}

func TestVerifyThenGateFail(t *testing.T) {
	ts := startVerifierHeaven(t)
	client := NewHeavenClient(ts.URL)
	v := NewVerifier(client)
	v.runCmd = mockRunCmd("FAIL", 2)

	result, err := v.Verify(VerifyRequest{MissionID: "m1", RepoRoot: t.TempDir(), Command: "test"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	if err := GateMerge(result.Receipt); err == nil {
		t.Fatal("gate should block failed verification")
	}
}

// ---------------------------------------------------------------------------
// Auto-detect with Verify
// ---------------------------------------------------------------------------

func TestVerifyAutoDetectCommand(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0o644)

	ts := startVerifierHeaven(t)
	client := NewHeavenClient(ts.URL)
	v := NewVerifier(client)

	var capturedCmd string
	v.runCmd = func(d, command string) (string, int, error) {
		capturedCmd = command
		return "ok", 0, nil
	}

	_, err := v.Verify(VerifyRequest{MissionID: "m1", RepoRoot: dir})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if capturedCmd != "go test ./..." {
		t.Errorf("auto-detected command = %q, want 'go test ./...'", capturedCmd)
	}
}
