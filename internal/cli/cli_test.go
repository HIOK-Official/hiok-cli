package cli

import (
	"flag"
	"reflect"
	"testing"
)

// Flags written after positional arguments must still be parsed. Go's flag package
// stops at the first non-flag token, so without interleaved parsing
// `hiok keyvault set <id> <name> --value x` silently ignored --value.
func TestFlagsAfterPositionalsAreParsed(t *testing.T) {
	cases := []struct {
		name            string
		args            []string
		wantValue       string
		wantPositionals []string
	}{
		{"flags last", []string{"vault-id", "item", "--value", "x"}, "x", []string{"vault-id", "item"}},
		{"flags first", []string{"--value", "x", "vault-id", "item"}, "x", []string{"vault-id", "item"}},
		{"flags in the middle", []string{"vault-id", "--value", "x", "item"}, "x", []string{"vault-id", "item"}},
		{"equals form", []string{"vault-id", "item", "--value=x"}, "x", []string{"vault-id", "item"}},
		{"no flags", []string{"vault-id", "item"}, "", []string{"vault-id", "item"}},
		{"no positionals", []string{"--value", "x"}, "x", nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			value := fs.String("value", "", "")

			positionals, err := parseInterleaved(fs, c.args)
			if err != nil {
				t.Fatalf("parse failed: %s", err)
			}
			if *value != c.wantValue {
				t.Errorf("--value = %q, want %q", *value, c.wantValue)
			}
			if !reflect.DeepEqual(positionals, c.wantPositionals) {
				t.Errorf("positionals = %q, want %q", positionals, c.wantPositionals)
			}
		})
	}
}

func TestSplitKeyValues(t *testing.T) {
	got := splitKeyValues([]string{"A=1", "B=two", "malformed", "C="})
	want := map[string]string{"A": "1", "B": "two", "C": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitKeyValues = %v, want %v", got, want)
	}
}

func TestSplitList(t *testing.T) {
	got := splitList(" canada , germany ,, ")
	want := []string{"canada", "germany"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitList = %q, want %q", got, want)
	}
}

// Every topic needs a name, a summary and at least one command, or it shows up in
// help as something the user cannot actually run.
func TestEveryTopicIsUsable(t *testing.T) {
	seen := map[string]bool{}
	for _, topic := range topics() {
		if topic.Name == "" || topic.Summary == "" {
			t.Errorf("topic %q is missing a name or summary", topic.Name)
		}
		if seen[topic.Name] {
			t.Errorf("topic %q is registered twice", topic.Name)
		}
		seen[topic.Name] = true

		if len(topic.Commands) == 0 {
			t.Errorf("topic %q has no commands", topic.Name)
		}
		for _, command := range topic.Commands {
			if command.Name == "" || command.Summary == "" {
				t.Errorf("%s has a command missing a name or summary", topic.Name)
			}
			if command.Run == nil {
				t.Errorf("%s %s has no implementation", topic.Name, command.Name)
			}
		}
	}
}

// A table must not crash on a record that is missing a column, which happens whenever
// the API grows a field or omits an optional one.
func TestTableToleratesMissingFields(t *testing.T) {
	p := Printer{Out: discard{}}
	rows := []map[string]any{{"name": "a"}, {"name": "b", "extra": 1}}
	if err := p.Table(rows, "name", "status", "count"); err != nil {
		t.Fatalf("Table failed on incomplete rows: %s", err)
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
