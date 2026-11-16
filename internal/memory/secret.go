package memory

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
)

// ErrSecretContent is returned when a memory looks like it carries a
// credential. Memories are injected into prompts and shown in the UI, so a
// secret stored here would leak far beyond where the user typed it.
var ErrSecretContent = errors.New("内容疑似包含密钥、密码或令牌，已拒绝写入记忆；请改用密码库保存凭据")

var (
	secretPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}`),
		regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}`),
		regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`),
		regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}`),
		regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`),
		regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}`),
		regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`),
		regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`),
		regexp.MustCompile(`(?i)\b(?:password|passwd|pwd|secret|api[_-]?key|access[_-]?token)\s*[=:：]\s*\S{3,}`),
		regexp.MustCompile(`(?:密码|口令|密钥)\s*[=:：]\s*\S{3,}`),
		regexp.MustCompile(`(?:密码|口令|密钥)\s*(?:是|为)\s*[!-~]{4,}`),
	}
	uuidPattern    = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	hexRunPattern  = regexp.MustCompile(`(?i)\b[0-9a-f]{32,}\b`)
	base64Run      = regexp.MustCompile(`[A-Za-z0-9+/_=-]{32,}`)
	hexLetterCheck = regexp.MustCompile(`(?i)[a-f]`)
)

// LooksLikeSecret reports whether text appears to contain a credential:
// well-known token formats, private key headers, password assignments with a
// value, or a long random-looking base64/hex run. UUIDs are identifiers, not
// secrets, and are ignored.
func LooksLikeSecret(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	for _, pattern := range secretPatterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	stripped := uuidPattern.ReplaceAllString(text, " ")
	for _, run := range hexRunPattern.FindAllString(stripped, -1) {
		if hexLetterCheck.MatchString(run) && strings.ContainsAny(run, "0123456789") {
			return true
		}
	}
	for _, run := range base64Run.FindAllString(stripped, -1) {
		if looksRandom(run) {
			return true
		}
	}
	return false
}

// looksRandom separates key material from long paths and identifiers, which
// use the same characters. Random base64 mixes upper, lower and digits and
// switches character class on roughly two of every three steps; words and
// path segments stay in one class for long stretches.
func looksRandom(run string) bool {
	var upper, lower, digit, switches int
	distinct := make(map[rune]struct{})
	previous := -1
	for _, r := range run {
		class := 3
		switch {
		case unicode.IsUpper(r):
			class = 0
			upper++
		case unicode.IsLower(r):
			class = 1
			lower++
		case unicode.IsDigit(r):
			class = 2
			digit++
		}
		if previous >= 0 && class != previous {
			switches++
		}
		previous = class
		distinct[r] = struct{}{}
	}
	if upper < 2 || lower < 2 || digit < 2 || len(distinct) < 12 {
		return false
	}
	return float64(switches)/float64(len(run)-1) >= 0.45
}
