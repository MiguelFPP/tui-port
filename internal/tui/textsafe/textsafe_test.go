package textsafe

import (
	"strings"
	"testing"
	"unicode"
)

func TestSanitizeNeutralizesTerminalSequences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain", in: "nginx", want: "nginx"},
		{name: "spanish", in: "camión", want: "camión"},
		{name: "csi", in: "\x1b[2Jnginx", want: " [2Jnginx"},
		{name: "osc52", in: "\x1b]52;c;SGVsbG8=\x07evil", want: " ]52;c;SGVsbG8= evil"},
		{name: "c1-csi", in: "\u009b2Jevil", want: " 2Jevil"},
		{name: "bel", in: "a\x07b", want: "a b"},
		{name: "newline", in: "a\nb", want: "a b"},
		{name: "tab", in: "a\tb", want: "a    b"},
		{name: "bidi", in: "a\u202eb", want: "a b"},
		{name: "line-separator", in: "a\u2028b", want: "a b"},
		{name: "zero-width", in: "a\u200bb", want: "a b"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := Sanitize(test.in)
			if got != test.want {
				t.Fatalf("Sanitize(%q) = %q, want %q", test.in, got, test.want)
			}
			if again := Sanitize(got); again != got {
				t.Fatalf("Sanitize is not idempotent: %q then %q", got, again)
			}
		})
	}
}

func TestSanitizeLeavesNoControlOrFormatRunes(t *testing.T) {
	t.Parallel()

	input := "\x00\x1b\x07\x9b\u009d\u202a\u202e\u200b\u2028\u2029\n\r\t fin"
	got := Sanitize(input)

	for _, r := range got {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			t.Fatalf("Sanitize(%q) left %U in %q", input, r, got)
		}
	}
	if !strings.Contains(got, "fin") {
		t.Fatalf("Sanitize(%q) = %q, want the printable text preserved", input, got)
	}
}

func FuzzSanitize(f *testing.F) {
	for _, seed := range []string{
		"nginx",
		"\x1b]52;c;SGVsbG8=\x07",
		"a\tb",
		"a\nb",
		"\u202eabc",
		"\x9b2J",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 1<<12 {
			return
		}

		got := Sanitize(input)
		for _, r := range got {
			if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
				t.Fatalf("Sanitize(%q) left %U", input, r)
			}
		}
		if again := Sanitize(got); again != got {
			t.Fatalf("Sanitize(%q) is not idempotent: %q then %q", input, got, again)
		}
	})
}
