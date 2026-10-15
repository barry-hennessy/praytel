package capture

// maxTrailingAttempts bounds how many brace-led candidates in the text before
// the object parseTrailingObject tries and rejects, so a line full of " {"
// costs a fixed amount of work rather than quadratic.
const maxTrailingAttempts = 8

// parseTrailingObject reads a line of text that ends in a JSON object of
// fields, such as zap's console encoder writes:
//
//	2026-10-14T16:41:49.412+0200	INFO	customer/server.go:64	HTTP request received	{"trace_id": "45c4…", "span_id": "…"}
//
// The object must follow a space or tab and run to the end of the line. It
// counts only if it names a trace or span, so a line that merely ends by
// printing a JSON payload stays plain text.
func parseTrailingObject(line string) result {
	if len(line) < 3 || line[len(line)-1] != '}' {
		return result{}
	}

	failed := 0
	for i := 1; i < len(line)-1 && failed < maxTrailingAttempts; i++ {
		if line[i] != '{' || (line[i-1] != ' ' && line[i-1] != '\t') {
			continue
		}

		f, n, ok := decodeObject(line[i:])
		switch {
		case !ok:
			failed++
		case i+n == len(line):
			if !f.joined() {
				return result{}
			}

			return result{fields: f, covered: n}
		default:
			// A complete object inside the text: step over it.
			i += n - 1
		}
	}

	return result{}
}
