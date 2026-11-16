package llm

import (
	"regexp"
	"strings"
)

// Some models now and then echo control tags from their own training format
// into an answer, e.g. "<budget:token_budget>200000</budget:token_budget>".
// They mean nothing to the reader, Markdown renders them as a broken link,
// and kept in the history they invite the model to repeat them. Only the
// budget: namespace is removed, so ordinary XML in an answer is untouched.
var (
	pseudoTagPair = regexp.MustCompile(`<budget:[A-Za-z_]+>[^<]{0,200}</budget:[A-Za-z_]+>`)
	pseudoTagLone = regexp.MustCompile(`</?budget:[A-Za-z_]+\s*/?>`)
)

// StripArtifacts removes control tags a model leaked into its answer.
func StripArtifacts(text string) string {
	if !strings.Contains(text, "budget:") {
		return text
	}
	text = pseudoTagPair.ReplaceAllString(text, "")
	text = pseudoTagLone.ReplaceAllString(text, "")
	return strings.TrimSpace(text)
}
