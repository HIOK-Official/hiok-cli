package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

// Printer renders results. Tables are for people; --json is for scripts, and is exact
// — it prints what the API returned rather than a reshaped view of it.
type Printer struct {
	JSON bool
	Out  io.Writer
}

func (p Printer) writer() io.Writer { return p.Out }

// Table prints selected fields of a list of records. Missing fields render empty
// rather than failing: a service that grows a field should not break older output.
func (p Printer) Table(rows []map[string]any, columns ...string) error {
	if p.JSON {
		return p.raw(rows)
	}
	if len(rows) == 0 {
		fmt.Fprintln(p.writer(), "No results.")
		return nil
	}
	if len(columns) == 0 {
		columns = inferColumns(rows[0])
	}

	tw := tabwriter.NewWriter(p.writer(), 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers(columns), "\t"))
	for _, row := range rows {
		cells := make([]string, 0, len(columns))
		for _, column := range columns {
			cells = append(cells, format(row[column]))
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	return tw.Flush()
}

// Record prints a single object as aligned key/value pairs.
func (p Printer) Record(record map[string]any, columns ...string) error {
	if p.JSON {
		return p.raw(record)
	}
	if record == nil {
		fmt.Fprintln(p.writer(), "No result.")
		return nil
	}
	if len(columns) == 0 {
		columns = inferColumns(record)
	}

	tw := tabwriter.NewWriter(p.writer(), 0, 0, 3, ' ', 0)
	for _, column := range columns {
		value, ok := record[column]
		if !ok {
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\n", header(column), format(value))
	}
	return tw.Flush()
}

func (p Printer) Message(format string, args ...any) {
	if p.JSON {
		return // a script asked for data, not commentary
	}
	fmt.Fprintf(p.writer(), format+"\n", args...)
}

func (p Printer) raw(value any) error {
	encoder := json.NewEncoder(p.writer())
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func inferColumns(record map[string]any) []string {
	keys := make([]string, 0, len(record))
	for key, value := range record {
		// Nested structures are unreadable in a table; --json shows them in full.
		switch value.(type) {
		case map[string]any, []any:
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > 8 {
		keys = keys[:8]
	}
	return keys
}

func headers(columns []string) []string {
	out := make([]string, 0, len(columns))
	for _, column := range columns {
		out = append(out, header(column))
	}
	return out
}

// header turns camelCase field names into readable column titles.
func header(name string) string {
	var b strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return strings.ToUpper(b.String())
}

func format(value any) string {
	switch v := value.(type) {
	case nil:
		return "-"
	case bool:
		if v {
			return "yes"
		}
		return "no"
	case float64:
		// JSON numbers are float64; render whole numbers without a decimal tail.
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%g", v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, format(item))
		}
		return strings.Join(parts, ",")
	case string:
		if v == "" {
			return "-"
		}
		return v
	default:
		return fmt.Sprint(v)
	}
}
