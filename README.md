# cli

`cli` is a **tiny, idiomatic Go library for subcommand-based CLIs** — modeled after the Go tool (`go mod init`, `go mod tidy`, etc.).

It provides:

* A `Command` type with per-command `flag.FlagSet`.
* Hierarchical subcommands (`go mod tidy`).
* Built-in `help` command and `usage` printing.
* Explicit error handling (`UsageError` vs runtime errors).
* No globals, no reflection, no dependencies.

---

## Install

```sh
go get github.com/jon-ski/cli
```

---

## Quick Start

```go
package main

import (
	"fmt"

	"github.com/jon-ski/cli"
)

func main() {
	// Root command: name left blank -> auto-filled from argv[0] (no .exe on Windows)
	root := cli.NewCommand("", "root tool", "[-v]")
	var verbose bool
	root.Flags.BoolVar(&verbose, "v", false, "verbose output")

	// "prog greet [name]"
	greet := cli.NewCommand("greet", "print a greeting", "[name]")
	greet.Run = func(ctx *cli.Context, args []string) error {
		name := "world"
		if len(args) == 1 {
			name = args[0]
		}
		if verbose {
			fmt.Fprintln(ctx.Stdout, "verbose: greeting about to print")
		}
		fmt.Fprintf(ctx.Stdout, "hello, %s!\n", name)
		return nil
	}
	root.Add(greet)

	// Run with standard os.Args, exit handling, and help.
	cli.Main(root)
}
```

### Usage

```sh
$ prog greet
hello, world!

$ prog greet alice
hello, alice!

$ prog -v greet bob
verbose: greeting about to print
hello, bob!

$ prog help
usage: prog [-v]

root tool

subcommands:
  greet  print a greeting

flags:
  -v    verbose output
```

---

## API

### `Command`

```go
type Command struct {
    Name  string
    Usage string
    Short string
    Long  string
    Flags *flag.FlagSet
    Run   func(ctx *Context, args []string) error
    Sub   []*Command
}
```

* `Name`: token for the command (`"mod"`, `"tidy"`).
* `Usage`: syntax string (`"[path]"`).
* `Short`: one-line summary.
* `Long`: longer help text.
* `Flags`: standard `*flag.FlagSet`, per command.
* `Run`: invoked after flag parse. Return `*UsageError` for usage mistakes; any other error prints and exits `1`.
* `Sub`: nested subcommands.

### `Context`

```go
type Context struct {
    Stdin  io.Reader
    Stdout io.Writer
    Stderr io.Writer
    Env    func(key string) string
}
```

Injected into `Run`. Lets you swap in buffers for testing.

### Errors

```go
type UsageError struct { Err error }
func Usagef(format string, a ...any) *UsageError
```

* Signals wrong usage → prints usage + exits `2`.
* All other errors print and exit `1`.

### Entrypoint

```go
func Main(root *Command)
```

* Dispatches on `os.Args`.
* Provides `help` as a command.
* Handles exit codes consistently.

If you leave `root.Name` blank, `cli.Main` sets it from argv[0] automatically and
removes any `.exe` suffix on Windows.

---

## Design Goals

* **Parallels Go’s own CLI** (`cmd/go`).
* **No globals**: you construct the tree yourself.
* **Testable**: call `Exec` directly with a `Context`.
* **Minimal**: zero dependencies, only stdlib.
* **Explicit errors**: no silent `os.Exit` in library code.

---

## Testing

Commands can be tested without `os.Exit`:

```go
ctx := &cli.Context{
    Stdin:  strings.NewReader(""),
    Stdout: &bytes.Buffer{},
    Stderr: &bytes.Buffer{},
    Env:    func(string) string { return "" },
}

err := root.Exec(ctx, []string{"greet", "test"})
if err != nil {
    t.Fatalf("unexpected error: %v", err)
}
got := ctx.Stdout.(*bytes.Buffer).String()
if got != "hello, test!\n" {
    t.Errorf("got %q", got)
}
```

---

## Example Hierarchy

```text
prog
 ├─ greet [name]
 └─ mod
     ├─ init [path]
     └─ tidy
```

Mirrors `go mod init`, `go mod tidy`.

---

## Exit Codes

* `0` success
* `1` runtime error
* `2` usage error (bad args/flags)

---

## Why not Cobra/urfave/kingpin?

Because this is **tiny, predictable, and Go-tool-like**:

* Cobra: large, reflection-heavy, YAML-friendly.
* urfave: global state, broad feature set.
* `cli`: **explicit tree, small surface, idiomatic flags**.

If you want similar to how `go` does it, this is it.

---

## License

MIT — do whatever you want, attribution appreciated.
