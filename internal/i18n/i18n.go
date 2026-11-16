// Package i18n localizes text the server generates (system messages,
// notifications). The client stores its language in account settings so the
// two stay consistent.
package i18n

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"superbot/internal/store"
)

const (
	Chinese = "zh"
	English = "en"
)

// Normalize maps arbitrary values to a supported language.
func Normalize(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), English) {
		return English
	}
	return Chinese
}

// Text returns the message for the given language.
func Text(language, chinese, english string) string {
	if Normalize(language) == English {
		return english
	}
	return chinese
}

// AccountLanguage reads the account's saved UI language. It falls back to
// Chinese when nothing is stored or the lookup fails.
func AccountLanguage(ctx context.Context, db *store.DB, accountID uuid.UUID) string {
	raw, err := db.GetSetting(ctx, "language:"+accountID.String())
	if err != nil {
		return Chinese
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return Chinese
	}
	return Normalize(value)
}
