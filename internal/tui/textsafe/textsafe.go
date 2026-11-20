// Package textsafe neutralizes untrusted text before it reaches a terminal.
//
// Process names, users and command lines come from /proc and may contain
// control characters. A terminal interprets escape sequences such as OSC 52
// (clipboard) or CSI (cursor and screen), so every string that is not a literal
// of the program must pass through Sanitize before being rendered.
package textsafe

import (
	"strings"
	"unicode"
)

// Sanitize replaces terminal control and Unicode format characters with a
// space. Tabs become four spaces, matching the command line renderer, and the
// result is safe to embed in a single styled line. It is idempotent.
func Sanitize(text string) string {
	if text == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(text))

	for _, r := range text {
		switch {
		case r == '\t':
			b.WriteString("    ")
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r), r == '\u2028', r == '\u2029':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}

	return b.String()
}
