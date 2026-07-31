package completion

// ReplacementRange identifies the portion of a reconstructed line produced by
// a completion item.
type ReplacementRange struct {
	Start int
	End   int
}

// ReconstructLine applies one result item to the shell word ending at pos.
// It is shared by interactive completion and speculative autosuggestions so
// both evaluate replacement-style candidates the same way.
func ReconstructLine(line string, pos int, result Result, item Item) (string, ReplacementRange, bool) {
	pos = clampCursor(line, pos)
	word := shellWordAt(line, pos)
	start := pos - len(word)
	if start < 0 {
		return "", ReplacementRange{}, false
	}
	replacement := result.Prefix + item.Value
	fullLine := line[:start] + replacement + line[pos:]
	return fullLine, ReplacementRange{Start: start, End: start + len(replacement)}, true
}
