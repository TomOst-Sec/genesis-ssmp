package heaven

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupFixtureRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Go file
	goSrc := `package sample

import "fmt"

func Greet(name string) string {
	return fmt.Sprintf("Hello, %s!", name)
}

func Farewell(name string) string {
	return fmt.Sprintf("Goodbye, %s!", name)
}

type Person struct {
	Name string
	Age  int
}

func NewPerson(name string, age int) Person {
	return Person{Name: name, Age: age}
}

func (p Person) SayHello() string {
	return Greet(p.Name)
}
`
	os.WriteFile(filepath.Join(dir, "sample.go"), []byte(goSrc), 0o644)

	// Python file
	pySrc := `class Calculator:
    def __init__(self, value=0):
        self.value = value

    def add(self, n):
        self.value += n
        return self

def factorial(n):
    if n <= 1:
        return 1
    return n * factorial(n - 1)

def main():
    calc = Calculator(10)
    result = factorial(5)
`
	os.WriteFile(filepath.Join(dir, "calc.py"), []byte(pySrc), 0o644)

	// TypeScript file
	tsSrc := `interface Shape {
    area(): number;
}

class Circle implements Shape {
    constructor(private radius: number) {}

    area(): number {
        return Math.PI * this.radius * this.radius;
    }
}

function createShape(): Shape {
    return new Circle(5);
}

const shape = createShape();
`
	os.WriteFile(filepath.Join(dir, "shapes.ts"), []byte(tsSrc), 0o644)

	return dir
}

func TestBuildIndex(t *testing.T) {
	dataDir := t.TempDir()
	repoDir := setupFixtureRepo(t)

	idx, err := NewIRIndex(dataDir)
	if err != nil {
		t.Fatalf("NewIRIndex: %v", err)
	}
	defer idx.Close()

	n, err := BuildIndex(context.Background(), idx, repoDir)
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	if n != 3 {
		t.Fatalf("BuildIndex indexed %d files, want 3", n)
	}

	stats, err := idx.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Files != 3 {
		t.Fatalf("Stats.Files = %d, want 3", stats.Files)
	}
	if stats.Symbols == 0 {
		t.Fatal("Stats.Symbols = 0, want > 0")
	}
}

func TestSymdef(t *testing.T) {
	dataDir := t.TempDir()
	repoDir := setupFixtureRepo(t)

	idx, err := NewIRIndex(dataDir)
	if err != nil {
		t.Fatalf("NewIRIndex: %v", err)
	}
	defer idx.Close()

	BuildIndex(context.Background(), idx, repoDir)

	// Go function
	syms, err := idx.Symdef("Greet")
	if err != nil {
		t.Fatalf("Symdef(Greet): %v", err)
	}
	if len(syms) == 0 {
		t.Fatal("Symdef(Greet) returned no results")
	}
	if syms[0].Kind != "function" {
		t.Fatalf("Greet kind = %q, want %q", syms[0].Kind, "function")
	}
	if !strings.HasSuffix(syms[0].Path, "sample.go") {
		t.Fatalf("Greet path = %q, want *sample.go", syms[0].Path)
	}

	// Python class
	syms, err = idx.Symdef("Calculator")
	if err != nil {
		t.Fatalf("Symdef(Calculator): %v", err)
	}
	if len(syms) == 0 {
		t.Fatal("Symdef(Calculator) returned no results")
	}
	if syms[0].Kind != "class" {
		t.Fatalf("Calculator kind = %q, want %q", syms[0].Kind, "class")
	}

	// TypeScript interface
	syms, err = idx.Symdef("Shape")
	if err != nil {
		t.Fatalf("Symdef(Shape): %v", err)
	}
	if len(syms) == 0 {
		t.Fatal("Symdef(Shape) returned no results")
	}
	if syms[0].Kind != "interface" {
		t.Fatalf("Shape kind = %q, want %q", syms[0].Kind, "interface")
	}
}

func TestCallers(t *testing.T) {
	dataDir := t.TempDir()
	repoDir := setupFixtureRepo(t)

	idx, err := NewIRIndex(dataDir)
	if err != nil {
		t.Fatalf("NewIRIndex: %v", err)
	}
	defer idx.Close()

	BuildIndex(context.Background(), idx, repoDir)

	// Greet is called in SayHello
	refs, err := idx.Callers("Greet", 10)
	if err != nil {
		t.Fatalf("Callers(Greet): %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("Callers(Greet) returned no results, expected call from SayHello")
	}

	// factorial has a recursive call
	refs, err = idx.Callers("factorial", 10)
	if err != nil {
		t.Fatalf("Callers(factorial): %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("Callers(factorial) returned no results")
	}
}

func TestSearch(t *testing.T) {
	dataDir := t.TempDir()
	repoDir := setupFixtureRepo(t)

	idx, err := NewIRIndex(dataDir)
	if err != nil {
		t.Fatalf("NewIRIndex: %v", err)
	}
	defer idx.Close()

	BuildIndex(context.Background(), idx, repoDir)

	syms, err := idx.Search("Greet", 10)
	if err != nil {
		t.Fatalf("Search(Greet): %v", err)
	}
	if len(syms) == 0 {
		t.Fatal("Search(Greet) returned no results")
	}

	// Partial match
	syms, err = idx.Search("eet", 10)
	if err != nil {
		t.Fatalf("Search(eet): %v", err)
	}
	if len(syms) == 0 {
		t.Fatal("Search(eet) returned no results, expected partial match on Greet")
	}
}

func TestSlice(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3\nline4\nline5\n"
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte(content), 0o644)

	// Get lines 2-4
	result, err := Slice(path, 2, 3)
	if err != nil {
		t.Fatalf("Slice: %v", err)
	}
	if result != "line2\nline3\nline4" {
		t.Fatalf("Slice(2,3) = %q, want %q", result, "line2\nline3\nline4")
	}

	// Past end of file
	result, err = Slice(path, 4, 100)
	if err != nil {
		t.Fatalf("Slice past end: %v", err)
	}
	if !strings.Contains(result, "line4") {
		t.Fatalf("Slice(4,100) should contain line4, got %q", result)
	}
}

func TestIncrementalReindex(t *testing.T) {
	dataDir := t.TempDir()
	repoDir := setupFixtureRepo(t)

	idx, err := NewIRIndex(dataDir)
	if err != nil {
		t.Fatalf("NewIRIndex: %v", err)
	}
	defer idx.Close()

	// First index
	n1, err := BuildIndex(context.Background(), idx, repoDir)
	if err != nil {
		t.Fatalf("BuildIndex(1): %v", err)
	}
	if n1 != 3 {
		t.Fatalf("first build indexed %d, want 3", n1)
	}

	// Second index without changes — should skip all files
	n2, err := BuildIndex(context.Background(), idx, repoDir)
	if err != nil {
		t.Fatalf("BuildIndex(2): %v", err)
	}
	if n2 != 0 {
		t.Fatalf("second build indexed %d files, want 0 (no changes)", n2)
	}
}

// --- Server endpoint tests ---

func newTestServerWithIR(t *testing.T) (*Server, string) {
	t.Helper()
	dataDir := t.TempDir()
	s, err := NewServer(dataDir)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return s, dataDir
}

func TestIRBuildEndpoint(t *testing.T) {
	s, _ := newTestServerWithIR(t)
	ts := httptest.NewServer(s)
	defer ts.Close()

	repoDir := setupFixtureRepo(t)

	resp, err := http.Post(ts.URL+"/ir/build", "application/json",
		strings.NewReader(`{"repo_path":"`+repoDir+`"}`))
	if err != nil {
		t.Fatalf("POST /ir/build: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var result IRBuildResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if result.FilesIndexed != 3 {
		t.Fatalf("files_indexed = %d, want 3", result.FilesIndexed)
	}
	if result.Stats.Symbols == 0 {
		t.Fatal("stats.symbols = 0")
	}
}

func TestIRSymdefEndpoint(t *testing.T) {
	s, _ := newTestServerWithIR(t)
	ts := httptest.NewServer(s)
	defer ts.Close()

	repoDir := setupFixtureRepo(t)
	http.Post(ts.URL+"/ir/build", "application/json",
		strings.NewReader(`{"repo_path":"`+repoDir+`"}`))

	resp, err := http.Get(ts.URL + "/ir/symdef?name=Greet")
	if err != nil {
		t.Fatalf("GET /ir/symdef: %v", err)
	}
	defer resp.Body.Close()

	var result IRSymdefResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Symbols) == 0 {
		t.Fatal("symdef returned no symbols")
	}
	if result.Symbols[0].Name != "Greet" {
		t.Fatalf("name = %q, want Greet", result.Symbols[0].Name)
	}
}

func TestIRCallersEndpoint(t *testing.T) {
	s, _ := newTestServerWithIR(t)
	ts := httptest.NewServer(s)
	defer ts.Close()

	repoDir := setupFixtureRepo(t)
	http.Post(ts.URL+"/ir/build", "application/json",
		strings.NewReader(`{"repo_path":"`+repoDir+`"}`))

	resp, err := http.Get(ts.URL + "/ir/callers?name=Greet&top_k=5")
	if err != nil {
		t.Fatalf("GET /ir/callers: %v", err)
	}
	defer resp.Body.Close()

	var result IRCallersResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Refs) == 0 {
		t.Fatal("callers returned no refs")
	}
}

func TestIRSliceEndpoint(t *testing.T) {
	s, _ := newTestServerWithIR(t)
	ts := httptest.NewServer(s)
	defer ts.Close()

	// Create a file to slice
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("a\nb\nc\nd\ne\n"), 0o644)

	resp, err := http.Get(ts.URL + "/ir/slice?path=" + path + "&start_line=2&n=3")
	if err != nil {
		t.Fatalf("GET /ir/slice: %v", err)
	}
	defer resp.Body.Close()

	var result IRSliceResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Content != "b\nc\nd" {
		t.Fatalf("content = %q, want %q", result.Content, "b\nc\nd")
	}
}

func TestIRSearchEndpoint(t *testing.T) {
	s, _ := newTestServerWithIR(t)
	ts := httptest.NewServer(s)
	defer ts.Close()

	repoDir := setupFixtureRepo(t)
	http.Post(ts.URL+"/ir/build", "application/json",
		strings.NewReader(`{"repo_path":"`+repoDir+`"}`))

	resp, err := http.Get(ts.URL + "/ir/search?q=Person&top_k=10")
	if err != nil {
		t.Fatalf("GET /ir/search: %v", err)
	}
	defer resp.Body.Close()

	var result IRSearchResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Symbols) == 0 {
		t.Fatal("search returned no symbols")
	}
}
