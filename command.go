package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Context carries I/O and environment access. Keep it tiny and swappable in tests.
type Context struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// Env returns the value of an environment variable (nil-safe when not set)
	Env func(key string) string
}

// UsageError indicates "you used the command wrong" (prints usage + exit code 2).
type UsageError struct {
	Err error
}

func (e *UsageError) Error() string { return e.Err.Error() }
func Usagef(format string, a ...any) *UsageError {
	return &UsageError{Err: fmt.Errorf(format, a...)}
}

// Command is a subcommand, mirroring cmd/go's style: a FlagSet, short/long help,
// optional children, and a Run func. Only the fields below are needed for a minimal, testable core.
type Command struct {
	// Name is the command name token (e.g., "mod", "init", "tidy")
	Name string

	// Usage is the usage line after the command path, e.g. "[-v] [path]".
	Usage string

	// Short is a one-line summary. Long is extended help (optional).
	Short string
	Long  string

	// Flags are parsed for this specific command before invoking run.
	Flags *flag.FlagSet

	// Run executes command logic. args are the remaining, non-flag args.
	// Return *UsageError for bad usage; any other error prints and returns exit code 1.
	Run func(ctx *Context, args []string) error

	// Sub holds nested subcommands (e.g., "go mod" has children "init", "tidy")
	Sub []*Command

	// parent is filled when added; used for building command paths and help.
	parent *Command
}

func NewCommand(name, short, usage string) *Command {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	// Send flag parsing errors to a buffer we control; cli will write usage consistently.
	// We redirect to io.Discard because we handle errors and print usage ourselves.
	fs.SetOutput(io.Discard)
	return &Command{
		Name:  name,
		Usage: usage,
		Short: short,
		Flags: fs,
	}
}

// Add registers a child subcommand and sets parent pointers.
func (c *Command) Add(children ...*Command) {
	for _, ch := range children {
		ch.parent = c
		c.Sub = append(c.Sub, ch)
	}
}

// Path returns the full command path tokens from root to this command.
func (c *Command) Path() []string {
	var p []*Command
	for cur := c; cur != nil; cur = cur.parent {
		p = append(p, cur)
	}

	// reverse
	for i, j := 0, len(p)-1; i < j; i, j = i+1, j-1 {
		p[i], p[j] = p[j], p[i]
	}
	out := make([]string, len(p))
	for i := range p {
		out[i] = p[i].Name
	}
	return out
}

// findSub returns the direct child matching name, or nil.
func (c *Command) findSub(name string) *Command {
	for _, ch := range c.Sub {
		if ch.Name == name {
			return ch
		}
	}
	return nil
}

// Exec dispatches args to subcommands, parses flags, and calls Run.
// It mirrors how cmd/go walks the command tree: first arg selects a child;
// if none, the current command's flags parse and Run executes.
func (c *Command) Exec(ctx *Context, args []string) error {
	// If we have subcommands and there is a next token, try dispatch.
	if len(c.Sub) > 0 && len(args) > 0 {
		if next := c.findSub(args[0]); next != nil {
			return next.Exec(ctx, args[1:])
		}
		// Special-case: "help" as a pseudo-subcommand anywhere.
		if args[0] == "help" {
			return printHelp(ctx, c, args[1:])
		}
	}

	// Parse flags for this command.
	if c.Flags != nil {
		if err := c.Flags.Parse(args); err != nil {
			// Flag parse failure -> usage error
			return &UsageError{Err: err}
		}
		args = c.Flags.Args()
	}

	// Help via -h / --help on any command.
	if c.Flags != nil {
		c.Flags.Visit(func(f *flag.Flag) {})
		// FlagSet doesn't expose whether -h was set if we didn't register it.
		// Register a standard help flag on each command for consistency.
	}

	// Ensure there is a -h registered once.
	if f := c.Flags.Lookup("h"); f == nil {
		c.Flags.Bool("h", false, "show help")
	}
	if f := c.Flags.Lookup("help"); f == nil {
		c.Flags.Bool("help", false, "show help")
	}
	// Re-parse only to capture -h/-help if supplied without other flags.
	// Note: reparse only when original parse consumed zero flags; keep minimal and safe.
	// To stay minimal, we just check os.Args form in Main; see Main below.

	// No Run means this is a pure namespace node (like "go mod") -> show help or usage.
	if c.Run == nil {
		return Usagef("no subcommand specified")
	}
	return c.Run(ctx, args)
}

// PrintUsage writes a concise usage block for c.
func (c *Command) PrintUsage(ctx *Context) {
	fmt.Fprintf(ctx.Stderr, "usage: %s %s\n", strings.Join(c.Path(), " "), strings.TrimSpace(c.Usage))
	if c.Short != "" {
		fmt.Fprintf(ctx.Stderr, "\n%s\n", c.Short)
	}
	if len(c.Sub) > 0 {
		fmt.Fprintln(ctx.Stderr, "\nsubcommands:")
		max := 0
		for _, ch := range c.Sub {
			if n := len(ch.Name); n > max {
				max = n
			}
		}
		for _, ch := range c.Sub {
			pad := strings.Repeat(" ", max-len(ch.Name))
			sum := ch.Short
			if sum == "" && ch.Long != "" {
				sum = firstLine(ch.Long)
			}
			fmt.Fprintf(ctx.Stderr, " %s%s  %s\n", ch.Name, pad, strings.TrimSpace(sum))
		}
	}
	if c.Flags != nil {
		// var b strings.Builder
		// c.Flags.SetOutput(&b)
		// c.Flags.PrintDefaults()
		// txt := strings.TrimSpace(b.String())
		// if txt != "" {
		// 	fmt.Fprintln(ctx.Stderr, "\nflags:")
		// 	fmt.Fprintln(ctx.Stderr, indent(txt, "  "))
		// }
		var b strings.Builder
		c.Flags.SetOutput(&b)
		c.Flags.PrintDefaults()
		txt := strings.TrimRight(b.String(), "\n") // keep original formatting (spaces + tabs)
		if strings.TrimSpace(txt) != "" {
			fmt.Fprintln(ctx.Stderr) // blank line
			fmt.Fprintln(ctx.Stderr, "flags:")
			// Print defaults verbatim to preserve consistent indentation from flag pkg
			fmt.Fprintln(ctx.Stderr, txt)
		}
	}
	if c.Long != "" {
		fmt.Fprintln(ctx.Stderr) // blank line
		fmt.Fprintln(ctx.Stderr, strings.TrimSpace(c.Long))
	}
}

// Main is a convenience entrypoint that executes root with os.Args and handles exits.
func Main(root *Command) {
	ctx := &Context{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Env:    os.Getenv,
	}

	argv := os.Args
	// Program name becomes root if unset.
	if root.Name == "" {
		base := filepath.Base(argv[0])
		// Strip .exe (Windows) or any other known extension
		if ext := filepath.Ext(base); ext != "" && (runtime.GOOS == "windows" || strings.EqualFold(ext, ".exe")) {
			base = strings.TrimSuffix(base, ext)
		}
		root.Name = base
	}
	// Provide a built-in "help" command at the root if user didn't add one.
	ensureHelp(root)

	// Support "prog help ..." anywhere, mirroring `go help`.
	if len(argv) >= 2 && argv[1] == "help" {
		if err := printHelp(ctx, root, argv[2:]); err != nil {
			fmt.Fprintln(ctx.Stderr, err)
			os.Exit(2)
		}
		return
	}
	// Add standard -h/--help handling for the top-level node:
	for _, tok := range argv[1:] {
		if tok == "-h" || tok == "--help" {
			root.PrintUsage(ctx)
			os.Exit(0)
		}
	}

	if err := root.Exec(ctx, argv[1:]); err != nil {
		switch e := err.(type) {
		case *UsageError:
			root.PrintUsage(ctx)
			if e.Err != nil && e.Err.Error() != "" {
				fmt.Fprintln(ctx.Stderr, "\nerror:", e.Err)
			}
			os.Exit(2)
		default:
			// Non-usage errors: concise print, exit 1.
			if !errors.Is(err, flag.ErrHelp) && err.Error() != "" {
				fmt.Fprintln(ctx.Stderr, err)
			}
			os.Exit(1)
		}
	}
}

func ensureHelp(root *Command) {
	if root.findSub("help") != nil {
		return
	}
	help := NewCommand("help", "show help for a command", "[command ...]")
	help.Run = func(ctx *Context, args []string) error {
		return printHelp(ctx, root, args)
	}
	root.Add(help)
}

func printHelp(ctx *Context, scope *Command, path []string) error {
	c := scope
	for _, p := range path {
		n := c.findSub(p)
		if n == nil {
			return Usagef("unknown command %q", p)
		}
		c = n
	}
	c.PrintUsage(ctx)
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func indent(s, pad string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}
