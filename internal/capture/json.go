package capture

import "encoding/json"

// parseJSONLine reads a line that is one JSON value, as written by JSON
// encoders such as zap's production logger, slog's JSONHandler and zerolog.
func parseJSONLine(line string) result {
	if line == "" {
		return result{}
	}

	switch line[0] {
	case '{':
		f, end, ok := decodeObject(line)
		if !ok || end != len(line) {
			return result{malformed: true}
		}

		return result{fields: f, covered: len(line)}
	case '[':
		if !json.Valid([]byte(line)) {
			return result{malformed: true}
		}

		return result{covered: len(line)}
	}

	return result{}
}
