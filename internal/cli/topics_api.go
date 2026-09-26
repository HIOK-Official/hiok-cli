package cli

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
)

// The API's full operation list, generated alongside the SDKs
// (hiok-sdk/generator/generate.py writes it; `make ops` copies it here).
//
//go:embed ops/operations.json
var operationsJSON []byte

type operation struct {
	Group   string `json:"group"`
	Name    string `json:"name"`
	HTTP    string `json:"http"`
	Path    string `json:"path"`
	Summary string `json:"summary"`
}

func loadOperations() ([]operation, error) {
	var ops []operation
	if err := json.Unmarshal(operationsJSON, &ops); err != nil {
		return nil, fmt.Errorf("embedded operation list is unreadable: %w", err)
	}
	return ops, nil
}

// findOperation accepts Group.Name (StorageAccount.GetStorageAccounts) in any case.
func findOperation(ops []operation, id string) (*operation, error) {
	group, name, ok := strings.Cut(id, ".")
	if !ok {
		return nil, fmt.Errorf("operation %q: expected Group.Name, e.g. StorageAccount.GetStorageAccounts (see `hiok api list`)", id)
	}
	for i := range ops {
		if strings.EqualFold(ops[i].Group, group) && strings.EqualFold(ops[i].Name, name) {
			return &ops[i], nil
		}
	}
	return nil, fmt.Errorf("no operation %q (see `hiok api list --search %s`)", id, name)
}

// keyValues is a repeatable --param k=v flag.
type keyValues map[string]string

func (kv keyValues) String() string { return "" }
func (kv keyValues) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok || k == "" {
		return fmt.Errorf("expected key=value, got %q", v)
	}
	kv[k] = val
	return nil
}

var pathParam = regexp.MustCompile(`\{([^}]+)\}`)

func apiTopic() Topic {
	var group, search, body string
	params := keyValues{}
	return Topic{
		Name:    "api",
		Summary: "Call any API operation — every resource and action the platform has",
		Commands: []Command{
			{
				Name:    "list",
				Summary: "List operations (filter with --group or --search)",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&group, "group", "", "only this group, e.g. StorageAccount")
					fs.StringVar(&search, "search", "", "text to find in the group, name or path")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					ops, err := loadOperations()
					if err != nil {
						return err
					}
					rows := []map[string]any{}
					needle := strings.ToLower(search)
					for _, op := range ops {
						if group != "" && !strings.EqualFold(op.Group, group) {
							continue
						}
						if needle != "" && !strings.Contains(strings.ToLower(op.Group+"."+op.Name+" "+op.Path), needle) {
							continue
						}
						rows = append(rows, map[string]any{"operation": op.Group + "." + op.Name, "method": op.HTTP, "path": op.Path})
					}
					sort.Slice(rows, func(i, j int) bool { return rows[i]["operation"].(string) < rows[j]["operation"].(string) })
					return app.Print.Table(rows, "operation", "method", "path")
				},
			},
			{
				Name:    "call",
				Summary: "Call an operation: hiok api call Group.Name [--param k=v]... [--body JSON|@file]",
				Flags: func(fs *flag.FlagSet) {
					fs.Var(params, "param", "a path or query parameter, key=value (repeatable)")
					fs.StringVar(&body, "body", "", "request body: JSON text, @file.json, or - for stdin")
				},
				Run: func(ctx context.Context, app *App, args []string) error {
					if len(args) != 1 {
						return fmt.Errorf("usage: hiok api call Group.Name [--param k=v]... [--body JSON|@file]")
					}
					if err := app.RequireToken(); err != nil {
						return err
					}
					ops, err := loadOperations()
					if err != nil {
						return err
					}
					op, err := findOperation(ops, args[0])
					if err != nil {
						return err
					}

					// Path placeholders come from --param; whatever is left is the query.
					rest := map[string]string{}
					for k, v := range params {
						rest[k] = v
					}
					var missing []string
					path := pathParam.ReplaceAllStringFunc(op.Path, func(m string) string {
						name := m[1 : len(m)-1]
						for k, v := range rest {
							if strings.EqualFold(k, name) {
								delete(rest, k)
								return url.PathEscape(v)
							}
						}
						missing = append(missing, name)
						return m
					})
					if len(missing) > 0 {
						return fmt.Errorf("%s needs --param %s", op.Group+"."+op.Name, strings.Join(missing, "=… --param ")+"=…")
					}
					if len(rest) > 0 {
						q := url.Values{}
						for k, v := range rest {
							q.Set(k, v)
						}
						path += "?" + q.Encode()
					}

					var payload []byte
					switch {
					case body == "":
					case body == "-":
						if payload, err = readAllStdin(); err != nil {
							return err
						}
					case strings.HasPrefix(body, "@"):
						if payload, err = os.ReadFile(body[1:]); err != nil {
							return err
						}
					default:
						payload = []byte(body)
					}
					if len(payload) > 0 && !json.Valid(payload) {
						return fmt.Errorf("--body is not valid JSON")
					}

					headers := map[string]string{}
					if len(payload) > 0 {
						headers["Content-Type"] = "application/json"
					} else if op.HTTP != http.MethodGet && op.HTTP != http.MethodDelete {
						payload = []byte("{}")
						headers["Content-Type"] = "application/json"
					}
					raw, err := app.Client.CallRaw(ctx, op.HTTP, path, payload, headers)
					if err != nil {
						return err
					}
					// Always the API's own JSON: the answer shapes differ too much for a table.
					var value any
					if json.Unmarshal(raw, &value) == nil {
						return app.Print.raw(value)
					}
					_, err = app.Print.writer().Write(append(raw, '\n'))
					return err
				},
			},
		},
	}
}

func readAllStdin() ([]byte, error) {
	var b strings.Builder
	buf := make([]byte, 32*1024)
	for {
		n, err := os.Stdin.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return []byte(b.String()), nil
}
