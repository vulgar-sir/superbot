package httpapi

import (
	"mime"
	"strings"
)

// contentDisposition builds a Content-Disposition header that keeps
// non-ASCII file names (Chinese titles) intact: an ASCII fallback for old
// clients plus the RFC 5987 UTF-8 form that browsers prefer.
func contentDisposition(disposition, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "file"
	}
	fallback := strings.Map(func(r rune) rune {
		switch {
		case r < 0x20 || r >= 0x7f || r == '"' || r == '\\' || r == '/' || r == ';':
			return '_'
		}
		return r
	}, name)
	return disposition + `; filename="` + fallback + `"; filename*=UTF-8''` + rfc5987Escape(name)
}

func rfc5987Escape(value string) string {
	const hex = "0123456789ABCDEF"
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || ('0' <= c && c <= '9') ||
			strings.IndexByte("!#$&+-.^_`|~", c) >= 0 {
			out.WriteByte(c)
			continue
		}
		out.WriteByte('%')
		out.WriteByte(hex[c>>4])
		out.WriteByte(hex[c&0x0f])
	}
	return out.String()
}

// inlineSafe reports whether a file of this type may be displayed inline on
// the app's own origin. Files can come from a bot, so anything a browser
// would execute (HTML, SVG, XML) is only ever served as a download.
func inlineSafe(mimeType string) bool {
	base, _, err := mime.ParseMediaType(mimeType)
	if err != nil {
		base = strings.ToLower(strings.TrimSpace(mimeType))
	}
	switch base {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp",
		"application/pdf", "text/plain":
		return true
	}
	return strings.HasPrefix(base, "audio/") || strings.HasPrefix(base, "video/")
}
