package god

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Receipt is a verification proof that a test command was executed.
// Matches proto/receipts.schema.json.
type Receipt struct {
	MissionID   string `json:"mission_id"`
	EnvHash     string `json:"env_hash"`
	CommandHash string `json:"command_hash"`
	StdoutHash  string `json:"stdout_hash"`
	ExitCode    int    `json:"exit_code"`
	Timestamp   string `json:"timestamp"`
}

// VerifyRequest describes what to verify and where.
type VerifyRequest struct {
	MissionID string
	RepoRoot  string
	Command   string // override auto-detected test command
}

// VerifyResult is the outcome of a verification run.
type VerifyResult struct {
	Receipt  Receipt `json:"receipt"`
	Passed   bool    `json:"passed"`
	Stdout   string  `json:"stdout"`
	BlobID   string  `json:"blob_id"` // blob ID of stored receipt in Heaven
}

// Verifier runs local tests and emits receipts stored in Heaven.
type Verifier struct {
	heaven  *HeavenClient
	runCmd  func(dir, command string) (stdout string, exitCode int, err error)
}

// NewVerifier creates a Verifier backed by the given Heaven client.
func NewVerifier(heaven *HeavenClient) *Verifier {
	return &Verifier{
		heaven: heaven,
		runCmd: defaultRunCmd,
	}
}

// Verify runs the test command, creates a receipt, stores it in Heaven,
// and returns the result.
func (v *Verifier) Verify(req VerifyRequest) (*VerifyResult, error) {
	// Step 1: Determine test command
	command := req.Command
	if command == "" {
		command = DetectTestCommand(req.RepoRoot)
	}

	// Step 2: Compute environment hash
	envHash := ComputeEnvHash(req.RepoRoot)

	// Step 3: Run the test command
	stdout, exitCode, runErr := v.runCmd(req.RepoRoot, command)
	if runErr != nil && exitCode == 0 {
		// Execution error (not a test failure) — propagate
		return nil, fmt.Errorf("verifier: run command: %w", runErr)
	}

	// Step 4: Build receipt
	receipt := Receipt{
		MissionID:   req.MissionID,
		EnvHash:     envHash,
		CommandHash: hashString(command),
		StdoutHash:  hashString(stdout),
		ExitCode:    exitCode,
		Timestamp:   nowFunc().UTC().Format(time.RFC3339),
	}

	// Step 5: Store receipt as blob in Heaven
	receiptJSON, err := json.Marshal(receipt)
	if err != nil {
		return nil, fmt.Errorf("verifier: marshal receipt: %w", err)
	}

	blobID, err := v.heaven.PutBlob(receiptJSON)
	if err != nil {
		return nil, fmt.Errorf("verifier: store receipt: %w", err)
	}

	// Step 6: Log verification event
	v.heaven.AppendEvent(map[string]any{
		"type":       "verification_complete",
		"mission_id": req.MissionID,
		"passed":     exitCode == 0,
		"exit_code":  exitCode,
		"blob_id":    blobID,
	})

	return &VerifyResult{
		Receipt: receipt,
		Passed:  exitCode == 0,
		Stdout:  stdout,
		BlobID:  blobID,
	}, nil
}

// GateMerge checks a receipt and returns nil if the verification passed,
// or an error describing why the merge should be blocked.
func GateMerge(receipt Receipt) error {
	if receipt.MissionID == "" {
		return fmt.Errorf("gate: receipt missing mission_id")
	}
	if receipt.ExitCode != 0 {
		return fmt.Errorf("gate: verification failed with exit code %d", receipt.ExitCode)
	}
	if receipt.Timestamp == "" {
		return fmt.Errorf("gate: receipt missing timestamp")
	}
	if _, err := time.Parse(time.RFC3339, receipt.Timestamp); err != nil {
		return fmt.Errorf("gate: invalid timestamp: %w", err)
	}
	return nil
}

// DetectTestCommand auto-detects the test command based on the project stack.
func DetectTestCommand(repoRoot string) string {
	// Go project
	if fileExists(filepath.Join(repoRoot, "go.mod")) {
		return "go test ./..."
	}
	// Node.js project
	if fileExists(filepath.Join(repoRoot, "package.json")) {
		return "npm test"
	}
	// Python project
	if fileExists(filepath.Join(repoRoot, "pytest.ini")) || fileExists(filepath.Join(repoRoot, "setup.py")) || fileExists(filepath.Join(repoRoot, "pyproject.toml")) {
		return "python -m pytest"
	}
	// Rust project
	if fileExists(filepath.Join(repoRoot, "Cargo.toml")) {
		return "cargo test"
	}
	// Fallback
	return "echo no test command detected"
}

// ComputeEnvHash hashes relevant lockfiles and tool versions to capture
// the execution environment. Best-effort: missing files are skipped.
func ComputeEnvHash(repoRoot string) string {
	h := sha256.New()

	// Hash lockfiles in deterministic order
	lockfiles := []string{
		"go.sum",
		"package-lock.json",
		"yarn.lock",
		"pnpm-lock.yaml",
		"Cargo.lock",
		"poetry.lock",
		"requirements.txt",
	}

	sort.Strings(lockfiles)
	for _, name := range lockfiles {
		data, err := os.ReadFile(filepath.Join(repoRoot, name))
		if err == nil {
			h.Write([]byte(name + ":"))
			h.Write(data)
			h.Write([]byte("\n"))
		}
	}

	// Hash tool version markers (best effort)
	toolFiles := []string{
		".go-version",
		".node-version",
		".nvmrc",
		".python-version",
		".tool-versions",
		"rust-toolchain.toml",
	}
	for _, name := range toolFiles {
		data, err := os.ReadFile(filepath.Join(repoRoot, name))
		if err == nil {
			h.Write([]byte(name + ":"))
			h.Write(data)
			h.Write([]byte("\n"))
		}
	}

	return hex.EncodeToString(h.Sum(nil))
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// defaultRunCmd executes a shell command and captures output.
func defaultRunCmd(dir, command string) (string, int, error) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return "", 1, fmt.Errorf("empty command")
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = dir

	output, err := cmd.CombinedOutput()
	stdout := string(output)

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return stdout, exitErr.ExitCode(), nil
		}
		return stdout, 1, err
	}

	return stdout, 0, nil
}
