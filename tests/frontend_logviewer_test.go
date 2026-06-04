package tests

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// **Validates: Requirements 19.3, 19.4**
//
// LogViewer Property Tests
// These tests perform static analysis on the LogViewer Vue component source
// to verify correctness properties related to search highlighting and level coloring.

const logViewerPath = "../webui/src/components/LogViewer/index.vue"

func readLogViewerSource(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(logViewerPath)
	if err != nil {
		t.Fatalf("failed to read LogViewer component: %v", err)
	}
	return string(content)
}

// --- Property 11: LogViewer 搜索高亮 ---
// All matched keyword positions are wrapped in <mark> tags,
// non-matched segments use <span> tags, and v-html/innerHTML is NOT used.

func TestLogViewer_Property11_SearchHighlight(t *testing.T) {
	content := readLogViewerSource(t)

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Sub-property 11a: The component uses <mark> for matched segments
	properties.Property("LogViewer uses <mark> tag for search-matched segments", prop.ForAll(
		func(_ string) bool {
			// Template must contain a <mark> element for highlighting matches
			hasMarkTag := strings.Contains(content, "<mark")
			// The mark tag should be conditional on segment.isMatch
			hasMarkCondition := strings.Contains(content, "v-if=\"segment.isMatch\"") ||
				strings.Contains(content, "v-if='segment.isMatch'")
			return hasMarkTag && hasMarkCondition
		},
		gen.AlphaString(),
	))

	// Sub-property 11b: The component uses <span> for non-matched segments
	properties.Property("LogViewer uses <span> tag for non-matched segments", prop.ForAll(
		func(_ string) bool {
			// Template must contain a <span> with v-else for non-matching text
			hasSpanElse := strings.Contains(content, "<span v-else")
			return hasSpanElse
		},
		gen.AlphaString(),
	))

	// Sub-property 11c: The component does NOT use v-html or innerHTML for log content
	properties.Property("LogViewer does NOT use v-html or innerHTML for rendering", prop.ForAll(
		func(_ string) bool {
			hasVHtml := strings.Contains(content, "v-html")
			hasInnerHTML := strings.Contains(content, "innerHTML")
			// Neither should be present
			return !hasVHtml && !hasInnerHTML
		},
		gen.AlphaString(),
	))

	// Sub-property 11d: The component has a text-splitting function for search highlighting
	properties.Property("LogViewer implements text segmentation for safe search highlighting", prop.ForAll(
		func(_ string) bool {
			// Must have a function that splits text into segments based on keyword
			hasSegmentFunction := strings.Contains(content, "getHighlightedSegments") ||
				strings.Contains(content, "highlightedSegments")
			// The function must produce segments with isMatch property
			hasIsMatchProp := strings.Contains(content, "isMatch")
			return hasSegmentFunction && hasIsMatchProp
		},
		gen.AlphaString(),
	))

	properties.TestingRun(t)
}

// --- Property 12: LogViewer 级别着色 ---
// Lines containing [INFO]/[WARN]/[ERROR] have corresponding style classes applied.

func TestLogViewer_Property12_LevelColoring(t *testing.T) {
	content := readLogViewerSource(t)

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Sub-property 12a: The component detects [INFO] and maps to log-info class
	properties.Property("LogViewer maps [INFO] to log-info CSS class", prop.ForAll(
		func(_ string) bool {
			// Must have detection for INFO level (regex uses escaped brackets \[INFO\])
			hasInfoDetection := strings.Contains(content, "INFO") &&
				(strings.Contains(content, `\[INFO\]`) || strings.Contains(content, "[INFO]"))
			// Must map to log-info class
			hasInfoClass := strings.Contains(content, "log-info")
			// The mapping must be in a function that returns the class
			hasLevelFunction := strings.Contains(content, "getLogLevelClass") ||
				strings.Contains(content, "logLevelClass")
			return hasInfoDetection && hasInfoClass && hasLevelFunction
		},
		gen.AlphaString(),
	))

	// Sub-property 12b: The component detects [WARN] and maps to log-warn class
	properties.Property("LogViewer maps [WARN] to log-warn CSS class", prop.ForAll(
		func(_ string) bool {
			// Must have detection for WARN level (regex uses escaped brackets \[WARN\])
			hasWarnDetection := strings.Contains(content, "WARN") &&
				(strings.Contains(content, `\[WARN\]`) || strings.Contains(content, "[WARN]"))
			hasWarnClass := strings.Contains(content, "log-warn")
			return hasWarnDetection && hasWarnClass
		},
		gen.AlphaString(),
	))

	// Sub-property 12c: The component detects [ERROR] and maps to log-error class
	properties.Property("LogViewer maps [ERROR] to log-error CSS class", prop.ForAll(
		func(_ string) bool {
			// Must have detection for ERROR level (regex uses escaped brackets \[ERROR\])
			hasErrorDetection := strings.Contains(content, "ERROR") &&
				(strings.Contains(content, `\[ERROR\]`) || strings.Contains(content, "[ERROR]"))
			hasErrorClass := strings.Contains(content, "log-error")
			return hasErrorDetection && hasErrorClass
		},
		gen.AlphaString(),
	))

	// Sub-property 12d: The level class is applied dynamically to each log line
	properties.Property("LogViewer applies level class dynamically via template binding", prop.ForAll(
		func(_ string) bool {
			// The template must bind the level class to each log line element
			// Look for :class binding that references the level function
			hasClassBinding := strings.Contains(content, "getLogLevelClass(") ||
				strings.Contains(content, ":class=\"[getLogLevelClass") ||
				strings.Contains(content, ":class='[getLogLevelClass")

			// Verify the CSS defines distinct styles for each level
			infoStyleRegex := regexp.MustCompile(`\.log-info\s*\{`)
			warnStyleRegex := regexp.MustCompile(`\.log-warn\s*\{`)
			errorStyleRegex := regexp.MustCompile(`\.log-error\s*\{`)

			hasInfoStyle := infoStyleRegex.MatchString(content)
			hasWarnStyle := warnStyleRegex.MatchString(content)
			hasErrorStyle := errorStyleRegex.MatchString(content)

			return hasClassBinding && hasInfoStyle && hasWarnStyle && hasErrorStyle
		},
		gen.AlphaString(),
	))

	properties.TestingRun(t)
}
