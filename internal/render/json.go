// Package render writes what commands produce: JSON for programs (design §5),
// and, from milestone 2, tables for people.
package render

import (
	"encoding/json"
	"io"
)

// JSON writes v as indented JSON followed by a newline. Indented because a
// person reads it too (piped to less, pasted in an issue); a program doesn't
// care either way. The trailing newline is what every Unix tool expects of a
// line of output, and what json.Encoder adds by itself.
//
// HTML escaping is off: by default Go writes "AT&T" as "AT\u0026T", which is
// valid JSON and right inside an HTML page, and wrong in a terminal.
func JSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
