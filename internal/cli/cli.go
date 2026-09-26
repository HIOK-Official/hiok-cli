package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	hiok "github.com/HIOK-Official/hiok-sdk/go"
)

// Command is one verb under a topic, e.g. `hiok keyvault create`.
type Command struct {
	Name    string
	Summary string
	// Run receives the flag set already parsed, so a command declares its own flags
	// in Flags and reads them in Run.
	Flags func(fs *flag.FlagSet)
	Run   func(ctx context.Context, app *App, args []string) error
}

// Topic groups the commands for one resource.
type Topic struct {
	Name     string
	Summary  string
	Commands []Command
}

// App carries everything a command needs: the API client, where output goes, and the
// configuration the invocation was resolved from.
type App struct {
	Client  *hiok.Client
	Config  *Config
	Print   Printer
	Topics  []Topic
	Program string
}

// Region resolves the region a command should act on: an explicit flag first, then
// the configured default, then canada.
func (a *App) Region(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if a.Config.Region != "" {
		return a.Config.Region
	}
	return "canada"
}

// RequireToken fails early with something actionable rather than letting the request
// come back as an opaque 401.
func (a *App) RequireToken() error {
	if a.Config.Token == "" && a.Client.ClientID == "" {
		return fmt.Errorf("not signed in — run `%s login` first, or set HIOK_TOKEN", a.Program)
	}
	return nil
}

// Run dispatches `hiok <topic> <command>`.
func Run(args []string, out *os.File) int {
	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	app := &App{
		Client:  hiok.New(cfg.Endpoint, cfg.Token),
		Config:  cfg,
		Print:   Printer{Out: out},
		Program: "hiok",
	}
	// A service principal in the environment (CI/CD) signs in on first use; an
	// explicit HIOK_TOKEN still wins.
	if os.Getenv("HIOK_TOKEN") == "" && os.Getenv("HIOK_CLIENT_ID") != "" {
		app.Client.ClientID = os.Getenv("HIOK_CLIENT_ID")
		app.Client.ClientSecret = os.Getenv("HIOK_CLIENT_SECRET")
		app.Client.Token = ""
	}
	app.Topics = topics()

	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		app.usage(out)
		return 0
	}

	if args[0] == "version" || args[0] == "--version" {
		fmt.Fprintf(out, "hiok %s\n", Version)
		return 0
	}

	topicName := args[0]
	topic := app.findTopic(topicName)
	if topic == nil {
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", topicName)
		app.usage(out)
		return 1
	}

	if len(args) == 1 {
		app.topicUsage(out, topic)
		return 1
	}

	commandName := args[1]
	var command *Command
	for i := range topic.Commands {
		if topic.Commands[i].Name == commandName {
			command = &topic.Commands[i]
			break
		}
	}
	if command == nil {
		fmt.Fprintf(os.Stderr, "unknown %s command %q\n\n", topic.Name, commandName)
		app.topicUsage(out, topic)
		return 1
	}

	fs := flag.NewFlagSet(topicName+" "+commandName, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "print the API response as JSON instead of a table")
	if command.Flags != nil {
		command.Flags(fs)
	}
	positionals, err := parseInterleaved(fs, args[2:])
	if err != nil {
		return 2
	}
	app.Print.JSON = *asJSON

	if err := command.Run(context.Background(), app, positionals); err != nil {
		fmt.Fprintln(os.Stderr, "error:", explain(err, app.Program))
		return 1
	}
	return 0
}

// explain turns the one failure everybody meets into the sentence that fixes it.
//
// Tokens expire, and the API says so as a bare 401 with an empty body — which
// reads like the command is broken rather than like the session is over. The
// token is on disk and looks present, so `RequireToken` passes and nothing else
// gets a chance to say what happened.
func explain(err error, program string) string {
	message := err.Error()
	if strings.Contains(message, "returned 401") {
		return fmt.Sprintf("your session has expired — run `%s login` to sign in again", program)
	}
	return message
}

// parseInterleaved parses flags that appear anywhere, not only before the first
// positional argument.
//
// Go's flag package stops at the first non-flag token, so `hiok keyvault set <id>
// <name> --value x` would silently ignore --value — and people write flags last far
// more often than first. This parses, takes one positional, and parses again until
// the arguments are exhausted.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var positionals []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positionals, nil
		}
		positionals = append(positionals, rest[0])
		args = rest[1:]
	}
}

func (a *App) findTopic(name string) *Topic {
	for i := range a.Topics {
		if a.Topics[i].Name == name {
			return &a.Topics[i]
		}
	}
	return nil
}

func (a *App) usage(out *os.File) {
	fmt.Fprintf(out, "hiok — command line for HIOK Cloud\n\n")
	fmt.Fprintf(out, "Usage:\n  %s <resource> <command> [flags]\n\n", a.Program)

	names := make([]string, 0, len(a.Topics))
	longest := 0
	for _, topic := range a.Topics {
		names = append(names, topic.Name)
		if len(topic.Name) > longest {
			longest = len(topic.Name)
		}
	}
	sort.Strings(names)

	fmt.Fprintln(out, "Resources:")
	for _, name := range names {
		topic := a.findTopic(name)
		fmt.Fprintf(out, "  %-*s  %s\n", longest, topic.Name, topic.Summary)
	}

	fmt.Fprintf(out, "\nRun `%s <resource>` to see its commands.\n", a.Program)
	fmt.Fprintf(out, "\nEvery command accepts --json to print the raw API response.\n")
	fmt.Fprintf(out, "\nConfiguration is read from %s, overridden by HIOK_ENDPOINT,\n", ConfigPath())
	fmt.Fprintf(out, "HIOK_TOKEN and HIOK_REGION.\n")
}

func (a *App) topicUsage(out *os.File, topic *Topic) {
	fmt.Fprintf(out, "%s — %s\n\n", topic.Name, topic.Summary)
	fmt.Fprintf(out, "Usage:\n  %s %s <command> [flags]\n\nCommands:\n", a.Program, topic.Name)

	longest := 0
	for _, command := range topic.Commands {
		if len(command.Name) > longest {
			longest = len(command.Name)
		}
	}
	for _, command := range topic.Commands {
		fmt.Fprintf(out, "  %-*s  %s\n", longest, command.Name, command.Summary)
	}
}

// splitKeyValues turns repeated key=value arguments into a map, which is how the CLI
// takes environment variables and tags.
func splitKeyValues(values []string) map[string]string {
	out := map[string]string{}
	for _, value := range values {
		key, val, found := strings.Cut(value, "=")
		if found && key != "" {
			out[key] = val
		}
	}
	return out
}

// splitList accepts a comma-separated flag value, trimming blanks.
func splitList(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// Version is stamped at build time; see the Makefile.
var Version = "dev"
