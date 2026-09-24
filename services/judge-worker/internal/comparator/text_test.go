package comparator

import "testing"

func TestTextEqual(t *testing.T) {
	tests := []struct {
		name             string
		expected, actual string
		equal            bool
	}{
		{"exact", "3\n", "3\n", true},
		{"line endings", "a\r\nb\r\n", "a\nb\n", true},
		{"trailing spaces", "a\nb", "a  \n b\t", false},
		{"line trailing spaces", "a\nb", "a  \nb\t\n\n", true},
		{"internal whitespace", "1 2", "1  2", false},
		{"empty", "\n", "", true},
	}
	comparator := NewText()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := comparator.Equal([]byte(test.expected), []byte(test.actual)); got != test.equal {
				t.Fatalf("Equal(%q, %q) = %v", test.expected, test.actual, got)
			}
		})
	}
}

func FuzzTextEqualReflexive(f *testing.F) {
	f.Add("a\r\nb  \n")
	f.Fuzz(func(t *testing.T, value string) {
		if !NewText().Equal([]byte(value), []byte(value)) {
			t.Fatalf("text comparison is not reflexive")
		}
	})
}
