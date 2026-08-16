package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	imgoci "github.com/imgoci/go"
)

const (
	// exitOK reports that the command did what it was asked to do.
	exitOK = 0
	// exitFailure reports a failure no library sentinel matched.
	exitFailure = 1
	// exitUsage reports a command line the CLI refused to run.
	exitUsage = 2
	// exitNotFound reports a failure that matched imgoci.ErrNotFound.
	exitNotFound = 3
	// exitUnauthorized reports a failure that matched imgoci.ErrUnauthorized.
	exitUnauthorized = 4
	// exitInvalidIndex reports a failure that matched imgoci.ErrInvalidIndex.
	exitInvalidIndex = 5
	// exitInvalidSpec reports a failure that matched imgoci.ErrInvalidSpec.
	exitInvalidSpec = 6
	// exitInvalidDest reports a failure that matched imgoci.ErrInvalidDest.
	exitInvalidDest = 7
	// exitDigestMismatch reports a failure that matched imgoci.ErrDigestMismatch.
	exitDigestMismatch = 8
	// exitUnsupportedType reports a failure that matched imgoci.ErrUnsupportedType.
	exitUnsupportedType = 9
	// exitSelectionMismatch reports a failure that matched imgoci.ErrSelectionMismatch.
	exitSelectionMismatch = 10
	// exitDecode reports a failure that matched imgoci.ErrDecode.
	exitDecode = 11
	// exitInterrupted reports a transfer stopped by SIGINT, by the shell
	// convention of 128 plus the signal number.
	exitInterrupted = 130
	// exitTerminated reports a transfer stopped by SIGTERM, by the same
	// convention.
	exitTerminated = 143
)

const (
	// flagArchitecture filters or selects io.imgoci.architecture.
	flagArchitecture = "architecture"
	// flagTarget filters or selects io.imgoci.target.
	flagTarget = "target"
	// flagRepresentation filters or selects io.imgoci.representation.
	flagRepresentation = "representation"
	// flagRole appends one required or selected role.
	flagRole = "role"
	// flagCompression appends one accepted compression, most preferred first.
	flagCompression = "compression"
	// flagCapability appends one consumer file-manifest type.
	flagCapability = "capability"
	// flagWorkers sets how many files move at once.
	flagWorkers = "workers"
	// flagPlainHTTP talks http:// to the registry instead of https://.
	flagPlainHTTP = "plain-http"
	// flagTimeout bounds how long the whole command may take.
	flagTimeout = "timeout"
	// flagProgress prints a progress line this often while a transfer runs.
	flagProgress = "progress"
)

const (
	// cmdPublish is the word that asks for an upload.
	cmdPublish = "publish"
	// cmdList is the word that asks for a deliverable listing.
	cmdList = "list"
	// cmdResolve is the word that asks for one selected deliverable.
	cmdResolve = "resolve"
	// cmdFetch is the word that asks for a verified download.
	cmdFetch = "fetch"
	// cmdHelp is the word that asks for usage text.
	cmdHelp = "help"
	// cmdVersion is the word that asks for the CLI identity line.
	cmdVersion = "version"
)

const (
	// resultPrecision is how finely a success line reports how long a transfer
	// took. Tenths of a second is all a human reads off it.
	resultPrecision = 100 * time.Millisecond
	// defaultWorkers is the library orchestrator default named in help text.
	defaultWorkers = 4
	// versionLine is what "imgoci version" writes. The CLI is unreleased, so
	// the line names the tool rather than a version number.
	versionLine = "imgoci (private reference CLI)\n"
	// twoOperands is how many operands fetch and publish take: a source and
	// a destination.
	twoOperands = 2
)

// errHelp reports that a command line asked for help rather than a transfer.
// It is not a failure: the usage text has already gone to standard output and
// the process leaves with zero.
var errHelp = errors.New("help requested")

// env is the process environment one run reads and writes.
//
// Injecting it is what makes the whole CLI testable in process: a test hands
// run arguments and two buffers and reads back the exact bytes the real
// program would have written.
type env struct {
	// args are the command line arguments after the program name.
	args []string
	// stdout receives machine data only: a publish digest, list and resolve
	// listings, and help or version that was asked for by name.
	stdout io.Writer
	// stderr receives everything else, including progress.
	stderr io.Writer
	// ticks, when set, replaces the ticker a -progress run would build, so a
	// test decides exactly when a progress line is rendered and how many are.
	// A real run leaves it nil and gets a [time.Ticker].
	ticks <-chan time.Time
	// now, when set, replaces the clock the progress line's elapsed column is
	// read off, so a rendered line can be asserted byte for byte. A real run
	// leaves it nil and gets [time.Now].
	now func() time.Time
}

// clock returns the clock a run reads: the injected one where a test supplied
// it, and the real one otherwise.
func (e env) clock() func() time.Time {
	if e.now != nil {
		return e.now
	}

	return time.Now
}

// run executes one command line and returns the exit code the process should
// leave with. It never writes anywhere but e and never exits the process.
func run(ctx context.Context, e env, sig *interrupts) int {
	e.stderr = guardStderr(e.stderr)

	if len(e.args) == 0 {
		return reportError(e, usageErrorf(topUsage(), `no command given; run "imgoci help" for the commands`), sig)
	}

	name, rest := e.args[0], e.args[1:]

	var err error
	switch name {
	case cmdPublish:
		err = runPublish(ctx, e, rest)
	case cmdList:
		err = runList(ctx, e, rest)
	case cmdResolve:
		err = runResolve(ctx, e, rest)
	case cmdFetch:
		err = runFetch(ctx, e, rest)
	case cmdHelp, "-h", "-help", "--help":
		err = runHelp(e, rest)
	case cmdVersion, "-version", "--version":
		err = runVersion(e, rest)
	default:
		err = usageErrorf(topUsage(), `unknown command %q; run "imgoci help" for the commands`, name)
	}

	if err == nil || errors.Is(err, errHelp) {
		return exitOK
	}

	return reportError(e, err, sig)
}

// runHelp answers a help request on standard output.
func runHelp(e env, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(e.stdout, topUsage())

		return nil
	}
	if len(args) > 1 {
		return usageErrorf(topUsage(), "help takes at most one command name")
	}

	switch args[0] {
	case cmdPublish:
		fmt.Fprint(e.stdout, publishUsage())
	case cmdList:
		fmt.Fprint(e.stdout, listUsage())
	case cmdResolve:
		fmt.Fprint(e.stdout, resolveUsage())
	case cmdFetch:
		fmt.Fprint(e.stdout, fetchUsage())
	case cmdVersion:
		fmt.Fprint(e.stdout, versionUsage())
	default:
		return usageErrorf(topUsage(), `unknown command %q; run "imgoci help" for the commands`, args[0])
	}

	return nil
}

// runVersion writes the identity line on standard output.
func runVersion(e env, args []string) error {
	if len(args) > 0 {
		return usageErrorf(versionUsage(), "version takes no operands")
	}
	fmt.Fprint(e.stdout, versionLine)

	return nil
}

// reportError writes the two-line failure presentation on standard error and
// returns the exit code the failure maps to.
//
// The first line preserves the library's printable error text and visibly
// escapes non-graphic runes so a peer cannot add log records or terminal
// controls. The second takes one of three documented forms: the sentinel
// [errors.Is] matched, the statement that none did, or the signal that stopped
// the run. It prints whatever the failure was: it is how a shell script watches
// the library's error classification work.
func reportError(e env, err error, sig *interrupts) int {
	fmt.Fprintf(e.stderr, "imgoci: %s\n", terminalSafeLine(err.Error()))

	var usage *usageError
	if errors.As(err, &usage) {
		fmt.Fprint(e.stderr, usage.usage)

		return exitUsage
	}

	// A recorded signal outranks the error's shape: whatever the cancellation
	// surfaced as — context.Canceled, a closed file, a reset socket — the run
	// stopped because it was told to, and the shell convention says so.
	if code, stopped := sig.exitStatus(); stopped {
		fmt.Fprintf(e.stderr, "imgoci: %s (exit %d)\n", stopDescription(code), code)

		return code
	}

	for _, entry := range sentinelExits() {
		if errors.Is(err, entry.err) {
			fmt.Fprintf(e.stderr, "imgoci: matched sentinel %s (exit %d)\n", entry.name, entry.code)

			return entry.code
		}
	}

	fmt.Fprintf(e.stderr, "imgoci: no sentinel matched (exit %d)\n", exitFailure)

	return exitFailure
}

// terminalSafeLine preserves graphic runes and renders every non-graphic rune
// with Go's visible escape spelling. The result cannot create another terminal
// line or execute a terminal control sequence.
func terminalSafeLine(value string) string {
	var b strings.Builder
	b.Grow(len(value))

	for len(value) > 0 {
		r, size := utf8.DecodeRuneInString(value)
		if r == utf8.RuneError && size == 1 {
			const hexadecimal = "0123456789abcdef"

			b.WriteString(`\x`)
			b.WriteByte(hexadecimal[value[0]>>4])
			b.WriteByte(hexadecimal[value[0]&0x0f])
			value = value[1:]

			continue
		}

		if unicode.IsGraphic(r) {
			b.WriteRune(r)
		} else {
			quoted := strconv.QuoteRuneToGraphic(r)
			b.WriteString(quoted[1 : len(quoted)-1])
		}
		value = value[size:]
	}

	return b.String()
}

// sentinelExits is the table that turns a failure into an exit code, checked
// with [errors.Is] in order, first match winning.
//
// It is built here rather than declared once because the package keeps no
// global state. Every sentinel the library exports has a row here, 3 through
// 11 with no gaps; nothing is held in reserve.
func sentinelExits() []sentinelExit {
	return []sentinelExit{
		{err: imgoci.ErrNotFound, code: exitNotFound, name: "imgoci.ErrNotFound"},
		{err: imgoci.ErrUnauthorized, code: exitUnauthorized, name: "imgoci.ErrUnauthorized"},
		{err: imgoci.ErrInvalidIndex, code: exitInvalidIndex, name: "imgoci.ErrInvalidIndex"},
		{err: imgoci.ErrInvalidSpec, code: exitInvalidSpec, name: "imgoci.ErrInvalidSpec"},
		{err: imgoci.ErrInvalidDest, code: exitInvalidDest, name: "imgoci.ErrInvalidDest"},
		{err: imgoci.ErrDigestMismatch, code: exitDigestMismatch, name: "imgoci.ErrDigestMismatch"},
		{err: imgoci.ErrUnsupportedType, code: exitUnsupportedType, name: "imgoci.ErrUnsupportedType"},
		{err: imgoci.ErrSelectionMismatch, code: exitSelectionMismatch, name: "imgoci.ErrSelectionMismatch"},
		{err: imgoci.ErrDecode, code: exitDecode, name: "imgoci.ErrDecode"},
	}
}

// newClient builds the library client the shared flags describe.
//
// The Docker credentials are always on and there is no flag for them. This is
// the command every other registry tool's users already know: log in with
// `docker login`, then run the tool. A test that needs a run with no
// credentials points DOCKER_CONFIG at an empty directory — which is what the
// library's own gates do.
func newClient(c commonFlags) (*imgoci.Client, error) {
	opts := []imgoci.Option{imgoci.WithDockerCredentials()}
	if c.plainHTTP {
		opts = append(opts, imgoci.WithPlainHTTP())
	}

	return imgoci.New(opts...)
}

// withDeadline runs one transfer under the deadline "-timeout" asked for, and
// labels a deadline that expired so the failure line says the run ran out of
// time rather than only that its context ended.
//
// A limit of zero means no deadline at all, which is what an unset flag leaves
// behind: the CLI adds no bound the caller did not ask for.
func withDeadline(ctx context.Context, limit time.Duration, transfer func(context.Context) error) error {
	if limit <= 0 {
		return transfer(ctx)
	}

	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()

	err := transfer(ctx)
	if err != nil && errors.Is(err, context.DeadlineExceeded) {
		return &timeoutError{limit: limit, err: err}
	}

	return err
}

// setFlagNames returns the names of the flags the command line actually set.
//
// [flag.FlagSet.Visit] walks only those, which is the whole mechanism behind
// the CLI's rule that unset means absent: a flag left alone contributes no
// option, so the library's own default applies and the CLI never restates it.
func setFlagNames(fs *flag.FlagSet) map[string]bool {
	set := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	return set
}

// usageErrorf builds a usage error whose complaint is the formatted message and
// whose usage block is the one the offending command prints.
func usageErrorf(usage, format string, args ...any) *usageError {
	return &usageError{message: fmt.Sprintf(format, args...), usage: usage}
}

// usageBlock renders one command's usage text: the header, then the flags fs
// declares in the shape [flag.FlagSet.PrintDefaults] writes them.
func usageBlock(header string, fs *flag.FlagSet) string {
	var b strings.Builder
	b.WriteString(header)

	fs.SetOutput(&b)
	fs.PrintDefaults()
	fs.SetOutput(io.Discard)

	return b.String()
}

// topUsage is the usage text for the CLI as a whole.
func topUsage() string {
	return `imgoci publishes, lists, resolves, and fetches imgoci releases. It is a
private reference command line interface: verification tooling, never
published and never released.

usage:
  imgoci publish [flags] <spec> <ref>   publish <spec>, print the index digest
  imgoci list    [flags] <ref>          list matching deliverables
  imgoci resolve [flags] <ref>          print one selected deliverable
  imgoci fetch   [flags] <ref> <dest>   fetch one selected deliverable into <dest>
  imgoci help    [command]              print this text, or one command's flags
  imgoci version                        print the CLI identity line

Flags must come before the operands, and "--" ends the flags for an operand
that begins with a dash. Run "imgoci help publish" (or list, resolve, fetch)
for what each command accepts.
`
}

// versionUsage is version's usage text.
func versionUsage() string {
	return `usage: imgoci version

Print the CLI identity line on standard output.
`
}

// newFlagSet builds the flag set for one subcommand.
//
// It writes nowhere: every byte this CLI emits goes through the injected
// writers, so the standard library's own error and usage printing is discarded
// and the errors it returns are reported by the CLI instead.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	return fs
}

// sentinelExit is one row of the table that turns a library failure into an
// exit code.
type sentinelExit struct {
	// err is the sentinel [errors.Is] is asked about.
	err error
	// code is the exit code the sentinel maps to.
	code int
	// name is how the failure line writes the sentinel, spelled the way a
	// caller would write it in Go.
	name string
}

// usageError reports a command line the CLI refused to run: an unknown flag, a
// value it could not parse, the wrong number of operands, a missing required
// filter. It exits 2 and prints the offending command's usage block, so the
// fix is on screen beside the complaint.
type usageError struct {
	// message is the complaint, written on its own line after the prefix.
	message string
	// usage is the usage block printed under the complaint.
	usage string
}

// Error returns the complaint. The usage block is presentation and stays out of
// it, so a caller that wraps this error does not drag a screen of text along.
func (e *usageError) Error() string {
	return e.message
}

// timeoutError reports that the deadline "-timeout" asked for expired before
// the transfer finished.
//
// It wraps the error the transfer returned, so [errors.Is] still answers for
// the whole chain and the sentinel table still sees whatever was underneath.
type timeoutError struct {
	// limit is the deadline the run was given.
	limit time.Duration
	// err is the error the transfer returned when the deadline expired.
	err error
}

// Error names the deadline first, then repeats the transfer's own error, so the
// failure line says why the run stopped as well as what it was doing.
func (e *timeoutError) Error() string {
	return "timed out after " + e.limit.String() + ": " + e.err.Error()
}

// Unwrap returns the error the transfer reported when the deadline expired.
func (e *timeoutError) Unwrap() error {
	return e.err
}

// commonFlags are the flags every registry command declares, and the values a
// parse left in them. Every one of them holds its type's zero value until the
// command line sets it.
type commonFlags struct {
	// timeout bounds the whole command, zero for no bound.
	timeout time.Duration
	// workers is how many files move at once.
	workers int
	// plainHTTP talks http:// to the registry instead of https://.
	plainHTTP bool
	// progress prints a progress line this often, zero for none.
	progress time.Duration
}

// register declares the shared flags on fs.
//
// Every default registered here is the zero value, not the library's, so
// [flag.FlagSet.PrintDefaults] adds no "(default 0)" beside a flag whose real
// default lives in the library. The help text names the real default instead.
func (c *commonFlags) register(fs *flag.FlagSet) {
	fs.BoolVar(&c.plainHTTP, flagPlainHTTP, false, "talk http:// to the registry instead of https://")
	fs.DurationVar(&c.timeout, flagTimeout, 0, "give up after this long, as 30s or 5m (unset: no limit)")
}

// registerProgress declares -progress on commands that move files.
func (c *commonFlags) registerProgress(fs *flag.FlagSet) {
	fs.DurationVar(&c.progress, flagProgress, 0,
		"print a progress line this often, as 5s or 500ms (unset: no progress output)")
}

// registerWorkers declares -workers on commands that move files.
func (c *commonFlags) registerWorkers(fs *flag.FlagSet) {
	fs.IntVar(&c.workers, flagWorkers, 0, fmt.Sprintf(
		"how many files to move at once (unset: the library default, %d)", defaultWorkers,
	))
}

// validate rejects shared flag values no command can run with. A negative
// duration is the same mistake on either flag: someone typed a sign by
// accident and should hear about it rather than watch a run go unbounded or
// silent. An explicit zero stays what leaving the flag alone means — no
// limit, and no progress lines. An explicit non-positive -workers is a
// usage error: the library would only reject it after a network round trip.
func (c *commonFlags) validate(set map[string]bool, name, usage string) error {
	if set[flagTimeout] && c.timeout < 0 {
		return usageErrorf(usage, "%s: -timeout must not be negative, got %s", name, c.timeout)
	}
	if set[flagProgress] && c.progress < 0 {
		return usageErrorf(usage, "%s: -progress must not be negative, got %s", name, c.progress)
	}
	if set[flagWorkers] && c.workers <= 0 {
		return usageErrorf(usage, "%s: -workers must be positive, got %d", name, c.workers)
	}

	return nil
}

// effectiveWorkers is the worker count the transfer will really run with: the
// flag where it was set, the library's own default where it was not.
func (c *commonFlags) effectiveWorkers(set map[string]bool) int {
	if set[flagWorkers] {
		return c.workers
	}

	return defaultWorkers
}

// settings renders the effective transport settings for a preflight line.
func (c *commonFlags) settings(set map[string]bool) string {
	rendered := fmt.Sprintf("workers=%d", c.effectiveWorkers(set))
	if c.plainHTTP {
		rendered += ", plain-http"
	}

	return rendered
}

// workerOptions returns [imgoci.WithWorkers] when -workers was set.
func (c *commonFlags) workerOptions(set map[string]bool) []imgoci.FetchOption {
	if !set[flagWorkers] {
		return nil
	}

	return []imgoci.FetchOption{imgoci.WithWorkers(c.workers)}
}

// publishWorkerOptions returns [imgoci.WithWorkers] when -workers was set.
func (c *commonFlags) publishWorkerOptions(set map[string]bool) []imgoci.PublishOption {
	if !set[flagWorkers] {
		return nil
	}

	return []imgoci.PublishOption{imgoci.WithWorkers(c.workers)}
}

// command describes one subcommand's shape so the shared parser can name it in
// every complaint it makes.
type command struct {
	// flags is the set that declares this command's flags.
	flags *flag.FlagSet
	// name is the word the caller typed.
	name string
	// syntax is the operand list the command takes, spelled as the usage line
	// spells it.
	syntax string
	// usage is the block printed under a complaint about this command.
	usage string
	// operands is how many operands the command takes.
	operands int
}

// parse parses args and returns the command's operands.
//
// A help request is answered here, on standard output, and reported as
// [errHelp] so the caller stops without a failure. Every other bad command line
// comes back as a [usageError] carrying this command's usage block.
func (c command) parse(e env, args []string) ([]string, error) {
	if err := c.flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(e.stdout, c.usage)

			return nil, errHelp
		}

		return nil, usageErrorf(c.usage, "%s: %s", c.name, err)
	}

	operands := c.flags.Args()
	if !sawTerminator(args, c.flags.NArg()) {
		if bad := misplacedFlag(operands); bad >= 0 {
			return nil, c.misplacedFlagError(operands, bad)
		}
	}
	if len(operands) != c.operands {
		return nil, usageErrorf(
			c.usage, "%s takes exactly %s, %s; got %d",
			c.name, operandWord(c.operands), c.syntax, len(operands),
		)
	}
	for i, operand := range operands {
		if operand == "" {
			return nil, usageErrorf(
				c.usage, "%s takes exactly %s, %s; operand %d is empty",
				c.name, operandWord(c.operands), c.syntax, i+1,
			)
		}
	}

	return operands, nil
}

// operandWord spells how many operands a command takes.
func operandWord(n int) string {
	if n == 1 {
		return "one operand"
	}

	return fmt.Sprintf("%d operands", n)
}

// misplacedFlagError complains about a flag written after the operands, and
// says where to move it. An operand that really does begin with a dash goes
// after a "--", which is the standard escape the complaint teaches.
func (c command) misplacedFlagError(operands []string, bad int) error {
	if bad == 0 {
		return usageErrorf(
			c.usage,
			"%s: flags must come before the operands, and %q is not one of them (write it after -- if it is an operand)",
			c.name,
			operands[bad],
		)
	}

	return usageErrorf(
		c.usage, "%s: flags must come before the operands; move %q before %q", c.name, operands[bad], operands[0],
	)
}

// interrupts records the terminating signal a run received, so the exit path
// can turn a cancelled transfer into the exit code the shell expects.
type interrupts struct {
	// mu guards received against the handler goroutine that writes it.
	mu sync.Mutex
	// received is the first terminating signal delivered, nil until one is.
	received os.Signal
}

// record notes the first terminating signal to arrive and ignores the rest,
// because only the first one cancels anything.
func (i *interrupts) record(s os.Signal) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.received == nil {
		i.received = s
	}
}

// exitStatus reports the exit code a signal-stopped run leaves with, and
// whether a signal stopped it at all. A nil receiver means no handler was ever
// installed, which is how the tests that do not care about signals call it.
func (i *interrupts) exitStatus() (int, bool) {
	if i == nil {
		return exitOK, false
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	switch i.received {
	case nil:
		return exitOK, false
	case syscall.SIGTERM:
		return exitTerminated, true
	default:
		return exitInterrupted, true
	}
}

// watchSignals installs the handler that turns the first interrupt into a
// cancelled transfer and lets the second one kill the process outright.
//
// The handler is written by hand rather than with [signal.NotifyContext]
// because NotifyContext keeps the signal registered after the first delivery: a
// second interrupt would be swallowed and a wedged transfer would be
// unkillable. Resetting the disposition after the first delivery hands the
// second one back to the default action.
//
// The handler never exits the process. It cancels, and the transfer unwinds on
// its own.
func watchSignals(cancel context.CancelFunc, stderr io.Writer) *interrupts {
	sig := &interrupts{}
	delivered := make(chan os.Signal, 1)
	signal.Notify(delivered, os.Interrupt, syscall.SIGTERM)

	go func() {
		received := <-delivered

		// Reset before anything that can block — cancel's bookkeeping, and
		// above all the stderr write, which stalls for as long as a full pipe
		// does. Until the disposition is back to the default, a second
		// interrupt lands in a channel nobody reads and kills nothing.
		signal.Reset(os.Interrupt, syscall.SIGTERM)
		sig.record(received)
		cancel()
		fmt.Fprintf(
			stderr, "imgoci: interrupted (%s), stopping; press Ctrl-C again to force quit\n", signalName(received),
		)
	}()

	return sig
}

// signalName renders a signal the way a shell names it, so the message and the
// exit code read as one story.
func signalName(s os.Signal) string {
	if s == syscall.SIGTERM {
		return "SIGTERM"
	}

	return "SIGINT"
}

// stopDescription renders the second failure line's account of a signal-stopped
// run, matched to the exit code the run leaves with.
func stopDescription(code int) string {
	if code == exitTerminated {
		return "terminated by SIGTERM"
	}

	return "interrupted by SIGINT"
}

// misplacedFlag returns the index of the first operand that looks like a flag,
// or -1 when none does.
//
// The standard flag package stops parsing at the first operand, so a flag
// written after one silently becomes an operand. Silently ignoring a flag on
// an instrument whose whole job is to show what happened is not acceptable, and
// no operand this CLI takes — a spec path, a reference, a destination path —
// begins with a dash unless the caller said so with "--".
func misplacedFlag(operands []string) int {
	for i, operand := range operands {
		if strings.HasPrefix(operand, "-") {
			return i
		}
	}

	return -1
}

// sawTerminator reports whether the parse that left narg operands behind got
// them from after a "--" terminator.
//
// The terminator is the standard way to say the rest are operands whatever they
// look like, and [flag.FlagSet.Parse] consumes it just before the operands it
// returns. When the caller wrote one, the misplaced-flag guard stands down: a
// dash-leading operand after "--" is exactly what the caller meant.
func sawTerminator(args []string, narg int) bool {
	before := len(args) - narg - 1

	return before >= 0 && args[before] == "--"
}
