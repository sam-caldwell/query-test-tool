package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var binaryPath string

func TestMain(m *testing.M) {
	// Build the binary once for all tests
	dir, err := os.MkdirTemp("", "sqlscore-test")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	binaryPath = filepath.Join(dir, "sqlscore")
	cmd := exec.Command("go", "build", "-o", binaryPath, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		panic("build failed: " + string(out) + ": " + err.Error())
	}

	os.Exit(m.Run())
}

func runCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}
	return stdout.String(), stderr.String(), exitCode
}

func runCLIWithStdin(t *testing.T, stdin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}
	return stdout.String(), stderr.String(), exitCode
}

func TestCLI_QueryFlag(t *testing.T) {
	stdout, _, exitCode := runCLI(t, "-q", "SELECT * FROM users")
	if exitCode != 0 {
		t.Fatalf("exit code: %d", exitCode)
	}
	if !strings.Contains(stdout, "Total Score") {
		t.Errorf("expected 'Total Score' in output, got: %s", stdout)
	}
}

func TestCLI_QueryFlagLong(t *testing.T) {
	stdout, _, exitCode := runCLI(t, "--query", "SELECT id FROM users WHERE id = 1")
	if exitCode != 0 {
		t.Fatalf("exit code: %d", exitCode)
	}
	if !strings.Contains(stdout, "Total Score") {
		t.Errorf("expected 'Total Score' in output, got: %s", stdout)
	}
}

func TestCLI_Stdin(t *testing.T) {
	stdout, _, exitCode := runCLIWithStdin(t, "SELECT * FROM users", "")
	// stdin with empty positional args
	stdout2, _, exitCode2 := runCLIWithStdin(t, "SELECT * FROM users")
	if exitCode != 0 && exitCode2 != 0 {
		t.Fatalf("exit codes: %d, %d", exitCode, exitCode2)
	}
	out := stdout + stdout2
	if !strings.Contains(out, "Total Score") && !strings.Contains(out, "total_score") {
		t.Errorf("expected score in output from stdin")
	}
}

func TestCLI_FileFlag(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "sqlscore-*.sql")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString("SELECT * FROM users ORDER BY name"); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	stdout, _, exitCode := runCLI(t, "-f", tmpFile.Name())
	if exitCode != 0 {
		t.Fatalf("exit code: %d", exitCode)
	}
	if !strings.Contains(stdout, "Total Score") {
		t.Errorf("expected 'Total Score' in output, got: %s", stdout)
	}
}

func TestCLI_JsonFormat(t *testing.T) {
	stdout, _, exitCode := runCLI(t, "-q", "SELECT * FROM users", "-format", "json")
	if exitCode != 0 {
		t.Fatalf("exit code: %d", exitCode)
	}
	if !strings.Contains(stdout, `"total_score"`) {
		t.Errorf("expected JSON output with total_score, got: %s", stdout)
	}
	if !strings.Contains(stdout, `"efficiency"`) {
		t.Errorf("expected efficiency in JSON output")
	}
	if !strings.Contains(stdout, `"memory_compute"`) {
		t.Errorf("expected memory_compute in JSON output")
	}
	if !strings.Contains(stdout, `"cognitive_complexity"`) {
		t.Errorf("expected cognitive_complexity in JSON output")
	}
}

func TestCLI_VerboseFlag(t *testing.T) {
	stdout, _, exitCode := runCLI(t, "-q", "SELECT * FROM users ORDER BY name", "-v")
	if exitCode != 0 {
		t.Fatalf("exit code: %d", exitCode)
	}
	if !strings.Contains(stdout, "select-star") {
		t.Errorf("verbose output should show rule names")
	}
}

func TestCLI_InvalidSQL(t *testing.T) {
	_, stderr, exitCode := runCLI(t, "-q", "NOT VALID SQL")
	if exitCode == 0 {
		t.Fatal("expected non-zero exit code for invalid SQL")
	}
	if !strings.Contains(stderr, "Error") {
		t.Errorf("expected error message in stderr, got: %s", stderr)
	}
}

func TestCLI_NoInput(t *testing.T) {
	// When run without stdin and no flags, should error
	cmd := exec.Command(binaryPath)
	cmd.Stdin = nil // no stdin pipe
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected error with no input")
	}
}

func TestCLI_MissingFile(t *testing.T) {
	_, stderr, exitCode := runCLI(t, "-f", "/nonexistent/file.sql")
	if exitCode == 0 {
		t.Fatal("expected non-zero exit code for missing file")
	}
	if !strings.Contains(stderr, "Error") {
		t.Errorf("expected error in stderr, got: %s", stderr)
	}
}

func TestCLI_InvalidFormat(t *testing.T) {
	_, stderr, exitCode := runCLI(t, "-q", "SELECT 1", "-format", "xml")
	if exitCode == 0 {
		t.Fatal("expected non-zero exit code for invalid format")
	}
	if !strings.Contains(stderr, "Error") {
		t.Errorf("expected error in stderr, got: %s", stderr)
	}
}

func TestCLI_CleanQuery(t *testing.T) {
	stdout, _, exitCode := runCLI(t, "-q", "SELECT id FROM users WHERE id = 1")
	if exitCode != 0 {
		t.Fatalf("exit code: %d", exitCode)
	}
	if !strings.Contains(stdout, "0") {
		t.Errorf("clean query should show score of 0")
	}
}

func TestCLI_GradeExcellent(t *testing.T) {
	stdout, _, _ := runCLI(t, "-q", "SELECT id FROM users WHERE id = 1")
	if !strings.Contains(stdout, "excellent") {
		t.Errorf("score 0 should show 'excellent' grade")
	}
}

func TestCLI_GradeNonExcellent(t *testing.T) {
	stdout, _, _ := runCLI(t, "-q", "SELECT * FROM users ORDER BY name")
	if strings.Contains(stdout, "excellent") {
		t.Errorf("query with issues should not be 'excellent'")
	}
}

func TestCLI_PositionalArg(t *testing.T) {
	stdout, _, exitCode := runCLI(t, "SELECT id FROM users WHERE id = 1")
	if exitCode != 0 {
		t.Fatalf("exit code: %d", exitCode)
	}
	if !strings.Contains(stdout, "Total Score") {
		t.Errorf("expected output from positional arg")
	}
}

func TestScoreGrade(t *testing.T) {
	tests := []struct {
		score int
		want  string
	}{
		{0, "excellent"},
		{5, "good"},
		{10, "good"},
		{15, "fair"},
		{25, "fair"},
		{30, "poor"},
		{50, "poor"},
		{51, "critical"},
		{100, "critical"},
	}

	for _, tt := range tests {
		got := scoreGrade(tt.score)
		if got != tt.want {
			t.Errorf("scoreGrade(%d) = %q, want %q", tt.score, got, tt.want)
		}
	}
}
