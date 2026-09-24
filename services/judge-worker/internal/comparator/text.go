package comparator

import (
	"bytes"
	"strings"
)

type Text struct{}

func NewText() *Text { return &Text{} }

func (*Text) Equal(expected, actual []byte) bool {
	return bytes.Equal(normalize(expected), normalize(actual))
}

func normalize(value []byte) []byte {
	value = bytes.ReplaceAll(value, []byte("\r\n"), []byte("\n"))
	value = bytes.ReplaceAll(value, []byte("\r"), []byte("\n"))
	lines := strings.Split(string(value), "\n")
	for index := range lines {
		lines[index] = strings.TrimRight(lines[index], " \t")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return []byte(strings.Join(lines, "\n"))
}
