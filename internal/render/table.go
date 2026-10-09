package render

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"unicode/utf8"
)

// Table writes rows under headers in columns, two spaces apart, for people.
// text/tabwriter does the aligning: each cell ends at a tab, and it pads the
// cells of a column to the widest. The last cell of a row has no tab after
// it, so no line ends in spaces.
func Table(w io.Writer, headers []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	// Nothing reaches w until Flush: tabwriter can't know a column's width
	// before it has seen every row.
	return tw.Flush()
}

// Or returns *s, or "-" when s is nil or blank: how a table shows a value
// Quantic doesn't have. Runs of spaces inside s collapse to one, because some
// names come from exchanges padded to a fixed width.
func Or(s *string) string {
	if s == nil {
		return "-"
	}
	if t := strings.Join(strings.Fields(*s), " "); t != "" {
		return t
	}
	return "-"
}

// AlignRight pads the cells of the columns cols on the left, so they line
// up on their right edge, as amounts should. The header counts toward the
// width. tabwriter can only align every column one way; this does one at a
// time, before Table pads them all on the right.
func AlignRight(headers []string, rows [][]string, cols ...int) {
	for _, c := range cols {
		width := utf8.RuneCountInString(headers[c])
		for _, row := range rows {
			width = max(width, utf8.RuneCountInString(row[c]))
		}
		headers[c] = pad(headers[c], width)
		for _, row := range rows {
			row[c] = pad(row[c], width)
		}
	}
}

func pad(s string, width int) string {
	return strings.Repeat(" ", width-utf8.RuneCountInString(s)) + s
}

// Fields writes label and value pairs, one a line, values lined up: the
// layout `quantic stock` uses too, for `quantic auth status`.
func Fields(w io.Writer, fields [][2]string) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, f := range fields {
		fmt.Fprintf(tw, "%s\t%s\n", f[0], f[1])
	}
	return tw.Flush()
}
