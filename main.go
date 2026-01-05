package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"regexp"
	"strconv"
	"strings"
)

// cleanPath normalizes a filesystem-like path without touching the filesystem.
func cleanPath(path string) string {
	if path == "" {
		return "."
	}

	isAbs := strings.HasPrefix(path, "/")
	parts := strings.Split(path, "/")

	// Pre-seed with an empty segment for absolute paths so joining adds the root slash.
	out := make([]string, 0, len(parts))
	if isAbs {
		out = append(out, "")
	}

	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}

		if part == ".." {
			// Prevent navigating above root for absolute paths.
			if len(out) > 0 {
				if len(out) == 1 && out[0] == "" {
					continue
				}
				if out[len(out)-1] != ".." {
					out = out[:len(out)-1]
					continue
				}
			}
			if !isAbs {
				out = append(out, "..")
			}
			continue
		}

		out = append(out, part)
	}

	if len(out) == 0 {
		if isAbs {
			return "/"
		}
		return "."
	}

	if isAbs && len(out) == 1 && out[0] == "" {
		return "/"
	}

	return strings.Join(out, "/")
}

// stringList collects repeated flag values.
type stringList []string

// String returns a comma-separated view of the list.
func (s *stringList) String() string {
	return strings.Join(*s, ",")
}

// Set appends a new value to the list.
func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

// options holds parsed CLI options and resolved runtime data.
type options struct {
	readInput     bool
	tildeExpand   bool
	tildeUnexpand bool
	envExpand     bool
	envUnexpand   bool
	absolute      bool
	unabsolute    bool
	oldPattern    string
	newPattern    string
	user          string
	envNames      []string
	verbose       bool
	base          string
	parentRaw     string

	resolvedHome string
	resolvedUser string
	envAllowed   map[string]struct{}
	envOrder     []string
	envValues    map[string]string
	regex        *regexp.Regexp
	baseAbs      string
	parentLimit  int
	unlimitedUp  bool
}

// errHelp indicates the user requested help.
var errHelp = errors.New("help requested")

// run is the main execution path that parses inputs and writes results.
func run(args []string, r io.Reader, stdout, stderr io.Writer) int {
	opts, paths, err := parseArgs(args, stdout, stderr)
	if err != nil {
		if errors.Is(err, errHelp) {
			return 0
		}
		return 1
	}

	if !opts.readInput && len(paths) == 0 {
		printUsage(stderr)
		return 1
	}

	if opts.readInput {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			paths = append(paths, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(stderr, "cleanpath: reading stdin: %v\n", err)
			return 1
		}
	}

	for _, arg := range paths {
		final, logs := transformPathVerbose(arg, opts)
		if opts.verbose {
			for _, line := range logs {
				fmt.Fprintln(stderr, line)
			}
		}
		fmt.Fprintln(stdout, final)
	}

	return 0
}

// parseArgs parses CLI flags and validates option combinations.
func parseArgs(args []string, stdout, stderr io.Writer) (options, []string, error) {
	var opts options
	var envNames stringList
	var help bool
	flags := flag.NewFlagSet("cleanpath", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Usage = func() {}

	flags.BoolVar(&opts.readInput, "i", false, "read paths from stdin, one per line")
	flags.BoolVar(&opts.readInput, "stdin", false, "read paths from stdin, one per line")
	flags.BoolVar(&opts.tildeExpand, "t", false, "expand leading tilda")
	flags.BoolVar(&opts.tildeExpand, "tilda", false, "expand leading tilda")
	flags.BoolVar(&opts.tildeUnexpand, "T", false, "unexpand leading tilda")
	flags.BoolVar(&opts.tildeUnexpand, "untilda", false, "unexpand leading tilda")
	flags.BoolVar(&opts.envExpand, "e", false, "expand environment variables")
	flags.BoolVar(&opts.envExpand, "env", false, "expand environment variables")
	flags.BoolVar(&opts.envUnexpand, "E", false, "unexpand environment variables")
	flags.BoolVar(&opts.envUnexpand, "unenv", false, "unexpand environment variables")
	flags.BoolVar(&opts.absolute, "a", false, "make path absolute")
	flags.BoolVar(&opts.absolute, "absolute", false, "make path absolute")
	flags.BoolVar(&opts.unabsolute, "A", false, "make path relative")
	flags.BoolVar(&opts.unabsolute, "unabsolute", false, "make path relative")
	flags.StringVar(&opts.oldPattern, "o", "", "regex pattern to replace")
	flags.StringVar(&opts.oldPattern, "old", "", "regex pattern to replace")
	flags.StringVar(&opts.newPattern, "n", "", "replacement for -o pattern")
	flags.StringVar(&opts.newPattern, "new", "", "replacement for -o pattern")
	flags.StringVar(&opts.user, "u", "", "user name for tilda expansion")
	flags.StringVar(&opts.user, "user", "", "user name for tilda expansion")
	flags.StringVar(&opts.base, "b", ".", "base directory for absolute/relative paths")
	flags.StringVar(&opts.base, "base", ".", "base directory for absolute/relative paths")
	flags.StringVar(&opts.parentRaw, "p", "0", "maximum number of parent traversals")
	flags.StringVar(&opts.parentRaw, "parent", "0", "maximum number of parent traversals")
	flags.Var(&envNames, "x", "environment variable name to expand (repeatable)")
	flags.Var(&envNames, "eXpand", "environment variable name to expand (repeatable)")
	flags.BoolVar(&opts.verbose, "v", false, "verbose logging to stderr")
	flags.BoolVar(&opts.verbose, "verbose", false, "verbose logging to stderr")
	flags.BoolVar(&help, "h", false, "show help")
	flags.BoolVar(&help, "help", false, "show help")

	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(stderr, "cleanpath: %v\n", err)
		printUsage(stderr)
		return options{}, nil, err
	}

	opts.envNames = envNames

	if help {
		printUsage(stdout)
		return options{}, nil, errHelp
	}

	if err := prepareOptions(&opts); err != nil {
		fmt.Fprintf(stderr, "cleanpath: %v\n", err)
		printUsage(stderr)
		return options{}, nil, err
	}

	return opts, flags.Args(), nil
}

// printUsage prints a brief CLI usage message.
func printUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: cleanpath [options] <path> [path ...]")
	fmt.Fprintln(w, "options:")
	fmt.Fprintln(w, "  -i, --stdin   read paths from stdin, one per line")
	fmt.Fprintln(w, "  -t, --tilda   expand leading tilda")
	fmt.Fprintln(w, "  -T, --untilda unexpand leading tilda")
	fmt.Fprintln(w, "  -e, --env     expand environment variables")
	fmt.Fprintln(w, "  -E, --unenv   unexpand environment variables")
	fmt.Fprintln(w, "  -a, --absolute    make path absolute")
	fmt.Fprintln(w, "  -A, --unabsolute  make path relative")
	fmt.Fprintln(w, "  -o, --old     regex pattern to replace")
	fmt.Fprintln(w, "  -n, --new     replacement for -o pattern")
	fmt.Fprintln(w, "  -u, --user    user name for tilda expansion")
	fmt.Fprintln(w, "  -b, --base    base directory for absolute/relative paths (default '.')")
	fmt.Fprintln(w, "  -p, --parent  maximum parent traversals for relative paths (default 0, '-' unlimited)")
	fmt.Fprintln(w, "  -x, --eXpand  environment variable name to expand (repeatable, '-' means all)")
	fmt.Fprintln(w, "  -v, --verbose verbose logging to stderr")
	fmt.Fprintln(w, "  -h, --help    show help and exit")
}

// prepareOptions validates option combinations and resolves derived data.
func prepareOptions(opts *options) error {
	if opts.tildeExpand && opts.tildeUnexpand {
		return fmt.Errorf("cannot use -t and -T together")
	}
	if opts.envExpand && opts.envUnexpand {
		return fmt.Errorf("cannot use -e and -E together")
	}
	if opts.absolute && opts.unabsolute {
		return fmt.Errorf("cannot use -a and -A together")
	}
	if opts.oldPattern != "" && opts.newPattern == "" {
		return fmt.Errorf("option -o requires -n")
	}
	if opts.newPattern != "" && opts.oldPattern == "" {
		return fmt.Errorf("option -n requires -o")
	}

	if opts.tildeExpand || opts.tildeUnexpand {
		home, name := resolveUserHome(opts.user)
		opts.resolvedHome = home
		opts.resolvedUser = name
	}

	if opts.envExpand || opts.envUnexpand {
		order, values := envOrderAndValues(opts.envNames, opts.envExpand)
		opts.envOrder = order
		opts.envValues = values
		opts.envAllowed = make(map[string]struct{}, len(order))
		for _, name := range order {
			opts.envAllowed[name] = struct{}{}
		}
	}

	if opts.parentRaw != "" {
		limit, unlimited, err := parseParentLimit(opts.parentRaw)
		if err != nil {
			return err
		}
		opts.parentLimit = limit
		opts.unlimitedUp = unlimited
	}

	if opts.absolute || opts.unabsolute {
		baseAbs, err := resolveBaseAbs(opts.base)
		if err != nil {
			return err
		}
		opts.baseAbs = baseAbs
	}

	if opts.oldPattern != "" {
		re, err := regexp.Compile(opts.oldPattern)
		if err != nil {
			return fmt.Errorf("invalid -o pattern: %v", err)
		}
		opts.regex = re
	}

	return nil
}

// transformPath applies enabled transformations in order.
func transformPath(path string, opts options) string {
	if opts.tildeExpand {
		path = expandTilde(path, opts)
	}
	if opts.tildeUnexpand {
		path = unexpandTilde(path, opts)
	}
	if opts.envExpand {
		path = expandEnv(path, opts.envAllowed)
	}
	if opts.envUnexpand {
		path = unexpandEnv(path, opts.envOrder, opts.envValues)
	}
	path = cleanPath(path)
	if opts.absolute {
		path = makeAbsolute(path, opts.baseAbs)
	}
	if opts.unabsolute {
		path = makeRelative(path, opts.baseAbs, opts.parentLimit, opts.unlimitedUp)
	}
	if opts.regex != nil {
		path = opts.regex.ReplaceAllString(path, opts.newPattern)
	}
	return path
}

// transformPathVerbose applies transformations and returns verbose log lines.
func transformPathVerbose(path string, opts options) (string, []string) {
	logs := []string{formatLogLine("initial", path, "")}
	current := path
	next := current

	if opts.tildeExpand {
		next = expandTilde(current, opts)
		if next != current {
			logs = append(logs, formatLogLine("tilda", current, next))
		}
		current = next
	}

	if opts.tildeUnexpand {
		next = unexpandTilde(current, opts)
		if next != current {
			logs = append(logs, formatLogLine("untilda", current, next))
		}
		current = next
	}

	if opts.envExpand {
		next = expandEnv(current, opts.envAllowed)
		if next != current {
			logs = append(logs, formatLogLine("env", current, next))
		}
		current = next
	}

	if opts.envUnexpand {
		next = unexpandEnv(current, opts.envOrder, opts.envValues)
		if next != current {
			logs = append(logs, formatLogLine("unenv", current, next))
		}
		current = next
	}

	next = cleanPath(current)
	if next != current {
		logs = append(logs, formatLogLine("clean", current, next))
	}
	current = next

	if opts.absolute {
		next = makeAbsolute(current, opts.baseAbs)
		if next != current {
			logs = append(logs, formatLogLine("absolute", current, next))
		}
		current = next
	}

	if opts.unabsolute {
		next = makeRelative(current, opts.baseAbs, opts.parentLimit, opts.unlimitedUp)
		if next != current {
			logs = append(logs, formatLogLine("unabsolute", current, next))
		}
		current = next
	}

	if opts.regex != nil {
		next = opts.regex.ReplaceAllString(current, opts.newPattern)
		if next != current {
			logs = append(logs, formatLogLine("regex", current, next))
		}
		current = next
	}

	logs = append(logs, formatLogLine("final", current, ""))
	return current, logs
}

// formatLogLine formats a verbose log line with aligned step names.
func formatLogLine(step, from, to string) string {
	const stepWidth = 10
	if to == "" {
		return fmt.Sprintf("cleanpath %-*s %s", stepWidth, step, from)
	}
	return fmt.Sprintf("cleanpath %-*s %s -> %s", stepWidth, step, from, to)
}

// parseParentLimit parses the -p value and returns a limit and unlimited flag.
func parseParentLimit(raw string) (int, bool, error) {
	if raw == "-" {
		return 0, true, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		return 0, false, fmt.Errorf("invalid -p value: %q", raw)
	}
	return limit, false, nil
}

// resolveBaseAbs resolves the base path into an absolute, cleaned path.
func resolveBaseAbs(base string) (string, error) {
	if base == "" {
		base = "."
	}
	if strings.HasPrefix(base, "/") {
		return cleanPath(base), nil
	}
	pwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot resolve base: %v", err)
	}
	return cleanPath(pwd + "/" + base), nil
}

// makeAbsolute returns an absolute path using the provided base when needed.
func makeAbsolute(path, baseAbs string) string {
	if path == "" {
		return cleanPath(path)
	}
	if strings.HasPrefix(path, "/") || baseAbs == "" {
		return path
	}
	return cleanPath(baseAbs + "/" + path)
}

// makeRelative returns a relative path from baseAbs when allowed by parent limits.
func makeRelative(path, baseAbs string, limit int, unlimited bool) string {
	if path == "" || !strings.HasPrefix(path, "/") || baseAbs == "" {
		return path
	}
	if path == baseAbs {
		return "."
	}
	pathSegs := splitAbs(path)
	baseSegs := splitAbs(baseAbs)
	commonLen := commonPrefixLen(pathSegs, baseSegs)
	parentsNeeded := len(baseSegs) - commonLen
	if !unlimited && parentsNeeded > limit {
		return path
	}

	relSegs := make([]string, 0, parentsNeeded+len(pathSegs)-commonLen)
	for i := 0; i < parentsNeeded; i++ {
		relSegs = append(relSegs, "..")
	}
	relSegs = append(relSegs, pathSegs[commonLen:]...)
	if len(relSegs) == 0 {
		return "."
	}
	return strings.Join(relSegs, "/")
}

// splitAbs splits an absolute path into segments.
func splitAbs(path string) []string {
	parts := strings.Split(path, "/")
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		segments = append(segments, part)
	}
	return segments
}

// commonPrefixLen finds the number of shared leading segments.
func commonPrefixLen(a, b []string) int {
	max := len(a)
	if len(b) < max {
		max = len(b)
	}
	n := 0
	for n < max && a[n] == b[n] {
		n++
	}
	return n
}

// resolveUserHome resolves the target user's home directory and name.
func resolveUserHome(userName string) (string, string) {
	currentName, currentHome := currentUser()
	if userName == "" {
		return currentHome, currentName
	}
	lookup, err := user.Lookup(userName)
	if err != nil {
		return currentHome, ""
	}
	return lookup.HomeDir, lookup.Username
}

// currentUser returns the current username and home directory, falling back to env vars.
func currentUser() (string, string) {
	lookup, err := user.Current()
	if err == nil {
		return lookup.Username, lookup.HomeDir
	}
	return os.Getenv("USER"), os.Getenv("HOME")
}

// expandTilde expands a leading tilda to a home directory.
func expandTilde(path string, opts options) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}

	slash := strings.Index(path, "/")
	var prefix string
	var rest string
	if slash == -1 {
		prefix = path[1:]
		rest = ""
	} else {
		prefix = path[1:slash]
		rest = path[slash:]
	}

	if prefix == "" {
		if opts.resolvedHome == "" {
			return path
		}
		return opts.resolvedHome + rest
	}

	lookup, err := user.Lookup(prefix)
	if err != nil || lookup.HomeDir == "" {
		return path
	}
	return lookup.HomeDir + rest
}

// unexpandTilde replaces a leading home directory with a tilda form.
func unexpandTilde(path string, opts options) string {
	if opts.resolvedHome == "" {
		return path
	}
	if path != opts.resolvedHome && !strings.HasPrefix(path, opts.resolvedHome+"/") {
		return path
	}

	rest := strings.TrimPrefix(path, opts.resolvedHome)
	prefix := "~"
	if opts.user != "" && opts.user != opts.resolvedUser {
		prefix = "~" + opts.user
	}

	return prefix + rest
}

var envPattern = regexp.MustCompile(`\$(\w+)|\$\{([^}]+)\}`)

// expandEnv expands $VAR and ${VAR} forms for allowed variables.
func expandEnv(path string, allowed map[string]struct{}) string {
	return envPattern.ReplaceAllStringFunc(path, func(match string) string {
		name := ""
		if strings.HasPrefix(match, "${") {
			name = match[2 : len(match)-1]
		} else {
			name = match[1:]
		}
		if name == "" {
			return match
		}
		if _, ok := allowed[name]; !ok {
			return match
		}
		value, ok := os.LookupEnv(name)
		if !ok {
			return match
		}
		return value
	})
}

// unexpandEnv replaces variable values with $NAME in a deterministic order.
func unexpandEnv(path string, order []string, values map[string]string) string {
	for _, name := range order {
		value := values[name]
		if value == "" {
			continue
		}
		path = strings.ReplaceAll(path, value, "$"+name)
	}
	return path
}

// envOrderAndValues resolves the env var order and values for expansion or unexpansion.
func envOrderAndValues(names []string, expandAll bool) ([]string, map[string]string) {
	if containsAllMarker(names) || (expandAll && len(names) == 0) {
		env := os.Environ()
		order := make([]string, 0, len(env))
		values := make(map[string]string, len(env))
		for _, entry := range env {
			parts := strings.SplitN(entry, "=", 2)
			key := parts[0]
			val := ""
			if len(parts) == 2 {
				val = parts[1]
			}
			order = append(order, key)
			values[key] = val
		}
		return order, values
	}

	if len(names) == 0 {
		return nil, map[string]string{}
	}

	order := make([]string, 0, len(names))
	values := make(map[string]string, len(names))
	for _, name := range names {
		order = append(order, name)
		values[name] = os.Getenv(name)
	}
	return order, values
}

// containsAllMarker checks if the "-" sentinel appears in the name list.
func containsAllMarker(names []string) bool {
	for _, name := range names {
		if name == "-" {
			return true
		}
	}
	return false
}

// main is the program entry point.
func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
