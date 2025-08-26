package cli_test

import (
	"bytes"
	"errors"
	"flag"
	"regexp"
	"strings"
	"testing"

	"github.com/jon-ski/cli"
)

type ioCtx struct {
	stdin  bytes.Buffer
	stdout bytes.Buffer
	stderr bytes.Buffer
}

func newCtx() (*cli.Context, *ioCtx) {
	bufs := &ioCtx{}
	ctx := &cli.Context{
		Stdin:  &bufs.stdin,
		Stdout: &bufs.stdout,
		Stderr: &bufs.stderr,
		Env:    func(string) string { return "" },
	}
	return ctx, bufs
}

func TestDispatchToNestedSubcommand(t *testing.T) {
	root := cli.NewCommand("prog", "root", "")
	mod := cli.NewCommand("mod", "module maintenance", "")
	init := cli.NewCommand("init", "initialize module", "[path]")
	var gotArgs []string
	init.Run = func(ctx *cli.Context, args []string) error {
		gotArgs = append(gotArgs, args...)
		return nil
	}
	root.Add(mod)
	mod.Add(init)

	ctx, _ := newCtx()
	if err := root.Exec(ctx, []string{"mod", "init", "example.com/x"}); err != nil {
		t.Fatalf("Exec returned error: %v", err)
	}
	if len(gotArgs) != 1 || gotArgs[0] != "example.com/x" {
		t.Fatalf("expected args [example.com/x], got %v", gotArgs)
	}
}

func TestUnknownTokenAtRootIsUsageError(t *testing.T) {
	root := cli.NewCommand("prog", "root", "")
	ctx, _ := newCtx()
	err := root.Exec(ctx, []string{"nope"})
	var u *cli.UsageError
	if !errors.As(err, &u) {
		t.Fatalf("expected UsageError, got %T:%v", err, err)
	}
}

func TestNamespaceNodeWithoutRunRequiresSubcommand(t *testing.T) {
	root := cli.NewCommand("prog", "root", "")
	ns := cli.NewCommand("mod", "module maintenance", "")
	root.Add(ns)

	ctx, _ := newCtx()
	err := root.Exec(ctx, []string{"mod"})
	var u *cli.UsageError
	if !errors.As(err, &u) {
		t.Fatalf("expected UsageError for missing subcommand, got %T:%v", err, err)
	}
	if !strings.Contains(u.Error(), "no subcommand specified") {
		t.Fatalf("unexpected usage error message: %q", u.Error())
	}
}

func TestPerCommandFlagParsingAndArgs(t *testing.T) {
	root := cli.NewCommand("prog", "root", "")

	// prog echo [-n] [words...]
	echo := cli.NewCommand("echo", "echo args", "[-n] [words...]")
	var noNewline bool
	echo.Flags.BoolVar(&noNewline, "n", false, "omit trailing newline")
	var got []string
	echo.Run = func(ctx *cli.Context, args []string) error {
		got = append(got, args...)
		return nil
	}
	root.Add(echo)

	ctx, _ := newCtx()
	err := root.Exec(ctx, []string{"echo", "-n", "a", "b", "c"})
	if err != nil {
		t.Fatalf("Exec returned error: %v", err)
	}
	if !noNewline {
		t.Fatalf("expected -n to set noNewline=true")
	}
	if strings.Join(got, ",") != "a,b,c" {
		t.Fatalf("unexpected args: %v", got)
	}
}

func TestFlagParseErrorIsUsageError(t *testing.T) {
	root := cli.NewCommand("prog", "root", "")
	cmd := cli.NewCommand("cmd", "does stuff", "")
	var x int
	cmd.Flags.IntVar(&x, "x", 0, "value")
	cmd.Run = func(ctx *cli.Context, args []string) error { return nil }
	root.Add(cmd)

	ctx, _ := newCtx()
	err := root.Exec(ctx, []string{"cmd", "-x", "NaN"})
	var u *cli.UsageError
	if !errors.As(err, &u) {
		t.Fatalf("expected UsageError for bad flag, got %T:%v", err, err)
	}
}

func TestRunErrorIsReturnedAsNonUsage(t *testing.T) {
	root := cli.NewCommand("prog", "root", "")
	fail := cli.NewCommand("fail", "always fails", "")
	want := errors.New("boom")
	fail.Run = func(ctx *cli.Context, args []string) error { return want }
	root.Add(fail)

	ctx, _ := newCtx()
	err := root.Exec(ctx, []string{"fail"})
	if err == nil {
		t.Fatalf("expected error")
	}
	var u *cli.UsageError
	if errors.As(err, &u) {
		t.Fatalf("expected non-usage error, got UsageError: %v", err)
	}
	if !errors.Is(err, want) {
		t.Fatalf("want %v, got %v", want, err)
	}
}

func TestUsagePrintIncludesSubcommandsAndFlags(t *testing.T) {
	root := cli.NewCommand("prog", "root", "[-v]")
	var verbose bool
	root.Flags.BoolVar(&verbose, "v", false, "verbose output")

	alpha := cli.NewCommand("alpha", "alpha short", "")
	beta := cli.NewCommand("beta", "", "")
	beta.Long = "beta long help\nwith details."
	root.Add(alpha, beta)

	ctx, bufs := newCtx()
	root.PrintUsage(ctx)

	out := bufs.stderr.String()
	// usage line
	if !strings.Contains(out, "usage: prog [-v]") {
		t.Fatalf("usage line missing or wrong:\n%s", out)
	}
	// flags block
	if !strings.Contains(out, "\nflags:\n") || !strings.Contains(out, "-v") {
		t.Fatalf("flags section missing:\n%s", out)
	}
	// subcommands table
	if !strings.Contains(out, "\nsubcommands:\n") {
		t.Fatalf("subcommands section missing:\n%s", out)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Fatalf("subcommands not listed:\n%s", out)
	}
	// long help fallback for beta summary
	if !strings.Contains(out, "beta") || !strings.Contains(out, "beta long help") {
		t.Fatalf("beta long help not surfaced in subcommand summary:\n%s", out)
	}
}

func TestPathReturnsFullHierarchy(t *testing.T) {
	root := cli.NewCommand("prog", "", "")
	a := cli.NewCommand("a", "", "")
	b := cli.NewCommand("b", "", "")
	c := cli.NewCommand("c", "", "")
	root.Add(a)
	a.Add(b)
	b.Add(c)

	got := strings.Join(c.Path(), "/")
	if got != "prog/a/b/c" {
		t.Fatalf("unexpected path: %q", got)
	}
}

func TestContextStdoutAndStderrAreUsed(t *testing.T) {
	root := cli.NewCommand("prog", "", "")
	hello := cli.NewCommand("hello", "", "")
	hello.Run = func(ctx *cli.Context, args []string) error {
		_, _ = ctx.Stdout.Write([]byte("hi\n"))
		return nil
	}
	root.Add(hello)

	ctx, bufs := newCtx()
	if err := root.Exec(ctx, []string{"hello"}); err != nil {
		t.Fatalf("Exec error: %v", err)
	}
	if bufs.stdout.String() != "hi\n" {
		t.Fatalf("unexpected stdout: %q", bufs.stdout.String())
	}
	if bufs.stderr.Len() != 0 {
		t.Fatalf("expected no stderr, got %q", bufs.stderr.String())
	}
}

func TestEachCommandHasAFlagSet(t *testing.T) {
	root := cli.NewCommand("prog", "", "")
	a := cli.NewCommand("a", "", "")
	b := cli.NewCommand("b", "", "")
	root.Add(a)
	a.Add(b)

	if root.Flags == nil || a.Flags == nil || b.Flags == nil {
		t.Fatalf("all commands should have a non-nil FlagSet")
	}
	// Ensure default output is not nil and is discardable without panicking when printing defaults.
	var bld strings.Builder
	a.Flags.SetOutput(&bld)
	a.Flags.PrintDefaults() // empty; should not panic
}

func TestHelpFlagsAreRegisteredPostParse(t *testing.T) {
	// Exec registers -h / -help on the command after parsing.
	root := cli.NewCommand("prog", "", "")
	c := cli.NewCommand("c", "", "")
	c.Run = func(ctx *cli.Context, args []string) error { return nil }
	root.Add(c)

	ctx, _ := newCtx()
	if err := root.Exec(ctx, []string{"c"}); err != nil {
		t.Fatalf("Exec error: %v", err)
	}
	if c.Flags.Lookup("h") == nil || c.Flags.Lookup("help") == nil {
		t.Fatalf("expected -h and -help flags to be present on command after Exec")
	}
}

func TestFlagParsingDoesNotLeakBetweenCommands(t *testing.T) {
	root := cli.NewCommand("prog", "", "")
	a := cli.NewCommand("a", "", "")
	b := cli.NewCommand("b", "", "")

	var fa bool
	a.Flags.BoolVar(&fa, "x", false, "flag on a")

	var fb bool
	b.Flags.BoolVar(&fb, "x", false, "flag on b")

	a.Run = func(ctx *cli.Context, args []string) error { return nil }
	b.Run = func(ctx *cli.Context, args []string) error { return nil }

	root.Add(a, b)

	ctx, _ := newCtx()
	if err := root.Exec(ctx, []string{"a", "-x"}); err != nil {
		t.Fatalf("Exec error: %v", err)
	}
	if !fa || fb {
		t.Fatalf("expected a.x=true, b.x=false; got fa=%v fb=%v", fa, fb)
	}

	// Reset and test the other way.
	fa, fb = false, false
	if err := root.Exec(ctx, []string{"b", "-x"}); err != nil {
		t.Fatalf("Exec error: %v", err)
	}
	if fa || !fb {
		t.Fatalf("expected a.x=false, b.x=true; got fa=%v fb=%v", fa, fb)
	}
}

func TestUsageErrorImplementsError(t *testing.T) {
	e := cli.Usagef("bad args")
	if e == nil || e.Error() == "" {
		t.Fatalf("Usagef should return non-nil with message")
	}
}

// Optional: sanity-check that a FlagSet created by NewCommand uses ContinueOnError,
// which is required for library-controlled usage printing.
func TestFlagSetMode(t *testing.T) {
	c := cli.NewCommand("x", "", "")
	// There is no direct accessor for the error handling mode.
	// We indirectly assert: passing an unknown flag returns error instead of exiting.
	var ran bool
	c.Run = func(ctx *cli.Context, args []string) error { ran = true; return nil }
	ctx, _ := newCtx()
	err := c.Exec(ctx, []string{"-unknown"})
	if err == nil {
		t.Fatalf("expected error for unknown flag")
	}
	// ensure we didn't run the command body
	if ran {
		t.Fatalf("command Run should not execute on flag parse failure")
	}
	var u *cli.UsageError
	if !errors.As(err, &u) {
		t.Fatalf("expected UsageError for unknown flag, got %T:%v", err, err)
	}
}

// Optional: ensure FlagSet output does not bleed to stderr during parsing,
// since the library routes flag errors through UsageError.
func TestFlagErrorsDoNotWriteDirectly(t *testing.T) {
	root := cli.NewCommand("prog", "", "")
	c := cli.NewCommand("c", "", "")
	var x int
	c.Flags.IntVar(&x, "x", 0, "int")
	c.Run = func(ctx *cli.Context, args []string) error { return nil }
	root.Add(c)

	ctx, bufs := newCtx()
	err := root.Exec(ctx, []string{"c", "-x=NaN"})
	if err == nil {
		t.Fatalf("expected parse error")
	}
	if bufs.stderr.Len() != 0 {
		t.Fatalf("flag parse should not have printed directly to stderr; got %q", bufs.stderr.String())
	}
}

// Bonus: demonstrate constructing a synthetic "help" output without using Main.
// We exercise PrintUsage for a nested command.
func TestPrintUsageForNestedCommand(t *testing.T) {
	root := cli.NewCommand("prog", "root short", "[-v]")
	child := cli.NewCommand("child", "child short", "[args]")
	root.Add(child)

	ctx, bufs := newCtx()
	child.PrintUsage(ctx)

	got := bufs.stderr.String()
	if !strings.Contains(got, "usage: prog child [args]") {
		t.Fatalf("expected full path in usage, got:\n%s", got)
	}
}

// Defensive: verify that FlagSet output can be captured by PrintUsage.
func TestPrintUsageIncludesFlagDefaults(t *testing.T) {
	cmd := cli.NewCommand("cmd", "short", "")
	var s string
	var n int
	cmd.Flags.StringVar(&s, "s", "def", "string flag")
	cmd.Flags.IntVar(&n, "n", 5, "int flag")
	cmd.Run = func(ctx *cli.Context, args []string) error { return nil }

	ctx, bufs := newCtx()
	cmd.PrintUsage(ctx)
	out := bufs.stderr.String()
	if !strings.Contains(out, "-s string") || !strings.Contains(out, "def") {
		t.Fatalf("missing -s flag/default in usage:\n%s", out)
	}
	if !strings.Contains(out, "-n int") || !strings.Contains(out, "5") {
		t.Fatalf("missing -n flag/default in usage:\n%s", out)
	}
}

// Ensure users can still use the standard library flag package with our FlagSet.
func TestFlagCompatibility(t *testing.T) {
	cmd := cli.NewCommand("cmd", "short", "")
	// Register a standard Flag value.
	var fs flag.Value = new(stringValue)
	cmd.Flags.Var(fs, "vstr", "value as flag.Value")
	cmd.Run = func(ctx *cli.Context, args []string) error { return nil }

	ctx, _ := newCtx()
	if err := cmd.Exec(ctx, []string{"-vstr=hello"}); err != nil {
		t.Fatalf("Exec error: %v", err)
	}
	if fs.String() != "hello" {
		t.Fatalf("expected vstr=hello, got %q", fs.String())
	}
}

type stringValue struct{ s string }

func (sv *stringValue) String() string { return sv.s }
func (sv *stringValue) Set(v string) error {
	sv.s = v
	return nil
}

func TestPrintUsage_LongHasNoLeadingSpace(t *testing.T) {
	cmd := cli.NewCommand("issue", "short", "[-out DIR] [flags]")
	cmd.Long = "Line1\nLine2"
	ctx, bufs := newCtx()
	cmd.PrintUsage(ctx)
	out := bufs.stderr.String()

	// Find the Long block start (after the single blank line).
	idx := strings.Index(out, "Line1")
	if idx == -1 {
		t.Fatalf("Long help not found in usage:\n%s", out)
	}
	// Assert no preceding space on that line.
	lineStart := idx - 1
	for lineStart >= 0 && out[lineStart] != '\n' {
		lineStart--
	}
	// at this point, out[lineStart] == '\n' or -1; next char should be 'L'
	if lineStart+1 < len(out) && out[lineStart+1] == ' ' {
		t.Fatalf("Long help starts with a space; got:\n%s", out[lineStart+1:idx+6])
	}
}

// Ensures PrintUsage prints the Long help block without a leading space
// and with exactly one blank line before it.
func TestPrintUsage_LongBlock_NoLeadingSpace(t *testing.T) {
	c := cli.NewCommand("prog", "short", "[-x]")
	c.Long = "This is the long help header.\nWith more details here."

	ctx := &cli.Context{
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
	}

	c.PrintUsage(ctx)
	out := ctx.Stderr.(*bytes.Buffer).String()

	// Find the long help header line.
	lines := strings.Split(out, "\n")

	var idx int = -1
	for i, ln := range lines {
		if strings.HasPrefix(ln, "This is the long help header.") {
			idx = i
			break
		}
	}
	if idx == -1 {
		t.Fatalf("long help header not found in output:\n%s", out)
	}

	// Assert no leading space on the first long line.
	if len(lines[idx]) > 0 && (lines[idx][0] == ' ' || lines[idx][0] == '\t') {
		t.Fatalf("long help line has leading whitespace: %q", lines[idx])
	}

	// Assert exactly one blank line precedes the long block (i.e., previous line is empty).
	if idx == 0 || strings.TrimSpace(lines[idx-1]) != "" {
		t.Fatalf("expected one blank line before long help; got context:\n...%q\n%q\n%q",
			lineSafe(lines, idx-2), lineSafe(lines, idx-1), lineSafe(lines, idx))
	}
}

// Ensures flag indentation is consistent: every flag definition line starts
// with exactly two spaces before the dash ("  -x"), and we don't produce
// extra-indented lines like "    -flag".
func TestPrintUsage_FlagIndentationConsistent(t *testing.T) {
	c := cli.NewCommand("prog", "short", "")
	var (
		s string
		i int
		b bool
	)
	c.Flags.StringVar(&s, "algo", "ed25519", "key algorithm")
	c.Flags.StringVar(&s, "ca-cert", "", "CA certificate path")
	c.Flags.StringVar(&s, "ca-key", "", "CA private key path")
	c.Flags.BoolVar(&b, "client", false, "include client EKU")
	c.Flags.StringVar(&s, "cn", "", "subject CommonName")
	c.Flags.IntVar(&i, "days", 397, "leaf validity in days")
	c.Flags.StringVar(&s, "dns", "localhost", "comma-separated DNS SANs")
	c.Flags.StringVar(&s, "ip", "", "comma-separated IP SANs")
	c.Flags.StringVar(&s, "name", "", "output base name")
	c.Flags.StringVar(&s, "org", "", "subject Organization")
	c.Flags.StringVar(&s, "out", "./certs", "output directory")
	c.Flags.BoolVar(&b, "server", true, "include server EKU")

	ctx := &cli.Context{
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
	}
	c.PrintUsage(ctx)
	out := ctx.Stderr.(*bytes.Buffer).String()

	// Extract the "flags:" block for inspection.
	flagsIdx := strings.Index(out, "\nflags:\n")
	if flagsIdx < 0 {
		t.Fatalf("flags section not found in output:\n%s", out)
	}
	flagsBlock := out[flagsIdx+len("\nflags:\n"):]
	// Stop at the next blank line (or end).
	if i := strings.Index(flagsBlock, "\n\n"); i >= 0 {
		flagsBlock = flagsBlock[:i]
	}

	lines := strings.Split(flagsBlock, "\n")

	// Compile regex patterns:
	//   goodFlagLine: exactly two spaces then '-' (e.g., "  -algo string")
	//   badExtraIndent: four spaces then '-' (e.g., "    -ca-cert string")
	goodFlagLine := regexp.MustCompile(`^  -[A-Za-z0-9][A-Za-z0-9\-]*\b`)
	badExtraIndent := regexp.MustCompile(`^ {4}-`)

	seenFlagLines := 0
	for _, ln := range lines {
		trim := strings.TrimSpace(ln)
		if trim == "" {
			continue
		}
		// Check only "flag definition" lines (those starting with spaces + '-').
		if strings.HasPrefix(ln, " ") && strings.Contains(ln, "-") && !strings.HasPrefix(trim, "-") {
			// Continuation/usage lines usually start with a TAB; skip them.
			// We only validate the definition lines.
			continue
		}
		if strings.HasPrefix(ln, "  -") {
			seenFlagLines++
			if !goodFlagLine.MatchString(ln) {
				t.Fatalf("flag definition line not matching expected pattern '  -name': %q", ln)
			}
			if badExtraIndent.MatchString(ln) {
				t.Fatalf("flag definition line has extra leading spaces: %q", ln)
			}
		}
		// Also assert we never produce a 4-space indent variant.
		if badExtraIndent.MatchString(ln) {
			t.Fatalf("found extra-indented flag line: %q", ln)
		}
	}

	if seenFlagLines == 0 {
		t.Fatalf("did not detect any flag definition lines in:\n%s", flagsBlock)
	}
}

// lineSafe protects against out-of-range indexes in failure messages.
func lineSafe(lines []string, idx int) string {
	if idx < 0 || idx >= len(lines) {
		return "<nil>"
	}
	return lines[idx]
}
