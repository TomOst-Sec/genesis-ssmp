package heaven

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// PromptSection is a typed chunk of a user prompt.
type PromptSection struct {
	Index       int    `json:"index"`
	SectionType string `json:"section_type"` // spec|constraints|glossary|style|examples|api|acceptance|security|other
	Title       string `json:"title"`
	Content     string `json:"content"`     // raw text, never rewritten
	SHA256      string `json:"sha256"`      // hash of Content bytes
	ByteOffset  int    `json:"byte_offset"`
	ByteLen     int    `json:"byte_len"`
}

// PromptSectioner splits a raw prompt into typed sections using a layered algorithm.
type PromptSectioner struct {
	MaxSectionBytes int // default 4096
}

// NewPromptSectioner creates a PromptSectioner with default settings.
func NewPromptSectioner() *PromptSectioner {
	return &PromptSectioner{MaxSectionBytes: 4096}
}

// Section splits raw prompt bytes into typed sections.
// Critical invariant: strings.Join(sections[i].Content) == string(raw) byte-for-byte.
func (ps *PromptSectioner) Section(raw []byte) []PromptSection {
	text := string(raw)

	// L0: Start with entire prompt as one section
	if len(text) == 0 {
		return []PromptSection{{
			Index:       0,
			SectionType: "other",
			Title:       "",
			Content:     "",
			SHA256:      hashStr(""),
			ByteOffset:  0,
			ByteLen:     0,
		}}
	}

	// L1+L2: Split on headings, respecting code fences
	chunks := ps.splitOnHeadings(text)

	// L3: Classify by sentinel phrases
	for i := range chunks {
		chunks[i].SectionType = classifySentinel(chunks[i].Title)
	}

	// L4: Split sections > MaxSectionBytes at paragraph boundaries
	chunks = ps.splitLargeSections(chunks)

	// Reindex and compute offsets
	offset := 0
	for i := range chunks {
		chunks[i].Index = i
		chunks[i].ByteOffset = offset
		chunks[i].ByteLen = len(chunks[i].Content)
		chunks[i].SHA256 = hashStr(chunks[i].Content)
		offset += chunks[i].ByteLen
	}

	return chunks
}

// splitOnHeadings splits text on markdown headings (#, ##, ###), keeping
// code fences (triple-backtick blocks) indivisible.
func (ps *PromptSectioner) splitOnHeadings(text string) []PromptSection {
	lines := strings.SplitAfter(text, "\n")
	// If text doesn't end with newline, last element won't have trailing \n
	// which is correct — we preserve raw bytes.

	var sections []PromptSection
	var currentContent strings.Builder
	currentTitle := ""
	inCodeFence := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track code fences (L1)
		if strings.HasPrefix(trimmed, "```") {
			inCodeFence = !inCodeFence
		}

		// L2: Split on headings, but not inside code fences
		if !inCodeFence && isHeading(trimmed) && currentContent.Len() > 0 {
			sections = append(sections, PromptSection{
				Title:   currentTitle,
				Content: currentContent.String(),
			})
			currentContent.Reset()
			currentTitle = extractHeadingTitle(trimmed)
		} else if !inCodeFence && isHeading(trimmed) && currentContent.Len() == 0 {
			currentTitle = extractHeadingTitle(trimmed)
		}

		currentContent.WriteString(line)
	}

	// Flush remaining
	if currentContent.Len() > 0 {
		sections = append(sections, PromptSection{
			Title:   currentTitle,
			Content: currentContent.String(),
		})
	}

	if len(sections) == 0 {
		sections = append(sections, PromptSection{
			Title:   "",
			Content: text,
		})
	}

	return sections
}

// splitLargeSections splits any section exceeding MaxSectionBytes at paragraph
// boundaries (double newline).
func (ps *PromptSectioner) splitLargeSections(sections []PromptSection) []PromptSection {
	var result []PromptSection
	for _, sec := range sections {
		if len(sec.Content) <= ps.MaxSectionBytes {
			result = append(result, sec)
			continue
		}
		// Split at paragraph boundaries (double newline)
		parts := splitAtParagraphs(sec.Content, ps.MaxSectionBytes)
		for i, part := range parts {
			title := sec.Title
			if i > 0 {
				title = sec.Title + " (cont.)"
			}
			result = append(result, PromptSection{
				SectionType: sec.SectionType,
				Title:       title,
				Content:     part,
			})
		}
	}
	return result
}

// splitAtParagraphs splits text into chunks no larger than maxBytes,
// preferring paragraph boundaries (\n\n).
func splitAtParagraphs(text string, maxBytes int) []string {
	if len(text) <= maxBytes {
		return []string{text}
	}

	var parts []string
	remaining := text

	for len(remaining) > maxBytes {
		// Find last paragraph boundary before maxBytes
		cutoff := remaining[:maxBytes]
		splitIdx := strings.LastIndex(cutoff, "\n\n")
		if splitIdx <= 0 {
			// No paragraph boundary found; fall back to last newline
			splitIdx = strings.LastIndex(cutoff, "\n")
		}
		if splitIdx <= 0 {
			// No newline at all; hard split
			splitIdx = maxBytes
		} else {
			splitIdx += 1 // include the newline in the first part
			if splitIdx < len(cutoff) && cutoff[splitIdx] == '\n' {
				splitIdx++ // include second \n of \n\n
			}
		}

		parts = append(parts, remaining[:splitIdx])
		remaining = remaining[splitIdx:]
	}

	if len(remaining) > 0 {
		parts = append(parts, remaining)
	}

	return parts
}

// isHeading returns true if the line starts with 1-3 '#' followed by a space.
func isHeading(line string) bool {
	if strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ") {
		return true
	}
	return false
}

// extractHeadingTitle returns the heading text without the '#' prefix.
func extractHeadingTitle(line string) string {
	line = strings.TrimLeft(line, "#")
	line = strings.TrimSpace(line)
	return line
}

// classifySentinel auto-sets section_type from heading title text.
func classifySentinel(title string) string {
	lower := strings.ToLower(title)

	// Order matters: more specific checks first
	sentinels := []struct {
		sectionType string
		keywords    []string
	}{
		{"constraints", []string{"constraint", "rule", "must", "never"}},
		{"acceptance", []string{"acceptance", "criteria", "pass", "verify"}},
		{"api", []string{"api", "endpoint", "contract", "interface"}},
		{"examples", []string{"example", "sample", "demo"}},
		{"security", []string{"security", "auth", "permission"}},
		{"style", []string{"style", "format", "convention"}},
		{"glossary", []string{"glossary", "definition", "term"}},
		{"spec", []string{"spec", "requirement", "overview", "summary"}},
	}

	for _, s := range sentinels {
		for _, kw := range s.keywords {
			if strings.Contains(lower, kw) {
				return s.sectionType
			}
		}
	}

	return "other"
}

func hashStr(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
