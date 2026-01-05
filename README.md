# cleanpath

Cleanpath normalizes filesystem-like paths without touching the filesystem. It can also expand or unexpand tilda (`~`), expand or unexpand environment variables, and apply a regex replace. Cleanpath was inspired by realpath. Cleanpath was mostly written by Codex.

## Usage

```
cleanpath [options] <path> [path ...]
```

You can also read paths from stdin with `-i`, one per line.

## Options

- `-i`, `--stdin` read paths from stdin, one per line
- `-t`, `--tilda` expand leading tilda
- `-T`, `--untilda` unexpand leading tilda
- `-e`, `--env` expand environment variables
- `-E`, `--unenv` unexpand environment variables
- `-a`, `--absolute` make path absolute
- `-A`, `--unabsolute` make path relative
- `-o`, `--old` regex pattern to replace
- `-n`, `--new` replacement for `-o` pattern
- `-u`, `--user USER` user name for tilda expansion
- `-b`, `--base DIR` base directory for absolute/relative paths (default `.`)
- `-p`, `--parent N` maximum parent traversals for relative paths (default `0`, `-` unlimited)
- `-x`, `--eXpand NAME` environment variable name to expand (repeatable, `-` means all)
- `-v`, `--verbose` verbose logging to stderr
- `-h`, `--help` show help and exit

Notes:
- `-t` and `-T` are mutually exclusive.
- `-e` and `-E` are mutually exclusive.
- `-o` requires `-n`, and `-n` requires `-o`.

## Behavior

Processing order for each path:
1) Tilda expand/unexpand
2) Env expand/unexpand
3) Path cleanup
4) Absolute/unabsolute
5) Regex replace

Tilda:
- Only a leading `~` is considered.
- `~user` uses OS user lookup.
- Unexpand uses `-u` to choose which user to match; it emits `~` only when the matched user equals `-u`.

Environment variables:
- Expansion supports `$VAR` and `${VAR}` (POSIX style only).
- If `-e` is set and no `-x` is provided, all environment variables are eligible.
- If `-E` is set and no `-x` is provided, no variables are unexpanded.
- `-x -` means all variables (for either expansion or unexpansion).
- For unexpansion, the order of `-x` flags controls replacement precedence.

Absolute/relative:
- `-a` leaves absolute paths unchanged; `-A` leaves relative paths unchanged.
- The base is resolved to an absolute path; if `--base` is relative it is treated as `$PWD/<base>` and cleaned.
- `-p 0` only produces relatives when the base is a prefix of the path.
- `-p -` allows any number of `..` segments.

## Examples

```
cleanpath /tmp/./aa//bb/
```

```
cleanpath -t ~/src/../bin
```

```
cleanpath -E -x HOME /home/me/projects
```

```
cleanpath -o 'aa+' -n 'a' /tmp/aaa/bb
```
