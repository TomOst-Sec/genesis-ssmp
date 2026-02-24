package detect

import (
	"os"
	"path/filepath"
)

// ProjectType represents the detected project type.
type ProjectType string

const (
	ProjectGo     ProjectType = "go"
	ProjectNode   ProjectType = "node"
	ProjectPython ProjectType = "python"
	ProjectRust   ProjectType = "rust"
	ProjectJava   ProjectType = "java"
	ProjectMake   ProjectType = "make"
	ProjectUnknown ProjectType = "unknown"
)

// ProjectInfo contains detected project information.
type ProjectInfo struct {
	Type         ProjectType
	TestCommand  string
	LintCommand  string
	BuildCommand string
	RootPath     string
}

// DetectProject scans the given directory and returns project info.
func DetectProject(dir string) ProjectInfo {
	info := ProjectInfo{
		Type:     ProjectUnknown,
		RootPath: dir,
	}

	if fileExists(filepath.Join(dir, "go.mod")) {
		info.Type = ProjectGo
		info.TestCommand = "go test ./..."
		info.LintCommand = "golangci-lint run"
		info.BuildCommand = "go build ./..."
		return info
	}

	if fileExists(filepath.Join(dir, "package.json")) {
		info.Type = ProjectNode
		info.TestCommand = "npm test"
		info.LintCommand = "npm run lint"
		info.BuildCommand = "npm run build"
		if fileExists(filepath.Join(dir, "yarn.lock")) {
			info.TestCommand = "yarn test"
			info.LintCommand = "yarn lint"
			info.BuildCommand = "yarn build"
		}
		if fileExists(filepath.Join(dir, "pnpm-lock.yaml")) {
			info.TestCommand = "pnpm test"
			info.LintCommand = "pnpm lint"
			info.BuildCommand = "pnpm build"
		}
		return info
	}

	if fileExists(filepath.Join(dir, "Cargo.toml")) {
		info.Type = ProjectRust
		info.TestCommand = "cargo test"
		info.LintCommand = "cargo clippy"
		info.BuildCommand = "cargo build"
		return info
	}

	if fileExists(filepath.Join(dir, "pyproject.toml")) || fileExists(filepath.Join(dir, "setup.py")) {
		info.Type = ProjectPython
		info.TestCommand = "pytest"
		info.LintCommand = "ruff check ."
		info.BuildCommand = "python -m build"
		if fileExists(filepath.Join(dir, "requirements.txt")) {
			info.LintCommand = "ruff check ."
		}
		return info
	}

	if fileExists(filepath.Join(dir, "pom.xml")) || fileExists(filepath.Join(dir, "build.gradle")) {
		info.Type = ProjectJava
		if fileExists(filepath.Join(dir, "pom.xml")) {
			info.TestCommand = "mvn test"
			info.LintCommand = "mvn checkstyle:check"
			info.BuildCommand = "mvn package"
		} else {
			info.TestCommand = "gradle test"
			info.LintCommand = "gradle check"
			info.BuildCommand = "gradle build"
		}
		return info
	}

	if fileExists(filepath.Join(dir, "Makefile")) {
		info.Type = ProjectMake
		info.TestCommand = "make test"
		info.LintCommand = "make lint"
		info.BuildCommand = "make"
		return info
	}

	return info
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Badge returns a short badge string for the project type.
func (p ProjectType) Badge() string {
	switch p {
	case ProjectGo:
		return "Go"
	case ProjectNode:
		return "Node.js"
	case ProjectPython:
		return "Python"
	case ProjectRust:
		return "Rust"
	case ProjectJava:
		return "Java"
	case ProjectMake:
		return "Make"
	default:
		return ""
	}
}
