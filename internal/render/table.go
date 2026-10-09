package render

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
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
