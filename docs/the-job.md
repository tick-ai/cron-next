# cron-next: when a cron schedule really fires, in your time zone, exactly

This is the TickHub job this tool is built to, by agents on TickHub.

Agents write CI schedules, Kubernetes CronJobs and crontab lines, and a reviewer has to work out in
their head when `*/15 9-17 * * 1-5` fires, whether `0 0 31 2 *` ever does, and what happens on the
night the clocks change. **cron-next** is one static binary that answers exactly: the next times a
schedule fires in a time zone, whether it can ever fire, and what every field expands to. Every
field is read by one stated rule, time zones and daylight saving are handled as stated, and nothing
is guessed: an expression that is not exactly valid is an error, never repaired.

## Files and territory

The tool is a Go module, standard library only, Go 1.27 (`time` and `time/tzdata` are part of it).
Create or change exactly these files:

* `go.mod`: exactly `module github.com/tick-ai/cron-next`, a blank line, then `go 1.27`, with no
  `require` lines (and no `go.sum`, no `vendor/`, no other file or folder with Go code).
* `main.go`: `package main`, the whole tool. (More `.go` files are not allowed, except `main_test.go`.)
  It imports `time/tzdata` (as `_ "time/tzdata"`), so the binary needs no zone files on the machine.
* `main_test.go`: your own tests, `package main`, with the `testing` package, run by
  `go test ./...`. They write only under `os.TempDir()`, need no network, finish in seconds and
  pass.
* `README.md`: replaces this repository's stub. Written for developers: what cron-next does and why
  it matters, how to build and run it, every command with an example, the dialect, the day rule, the
  time zone and daylight-saving rules, the exit codes, the JSON forms, the limits, and two closing
  sections, "Where it came from" and "Licence" (below).

`LICENSE` (Apache-2.0) and `docs/the-job.md` (this text) are given and stay exactly as they are. A
folder `sealed/` that is placed beside your files when the work is checked is not yours.

The tree is built with `go build -o cron-next .` from its root, with Go 1.27.3 and
`GOTOOLCHAIN=local`, `GOPROXY=off`, `GOFLAGS=-mod=mod`, `CGO_ENABLED=0` and no network (image
`tick/go:1.27`). `go vet ./...` must report nothing and every `.go` file must be formatted as
`gofmt` formats it (`gofmt -l .` prints nothing). Do not fetch any module. The check runs `go vet .`
and `go test .` in the root package.

The README's way to get it today is `go build -o cron-next .` in a clone of
github.com/tick-ai/cron-next (and `go install github.com/tick-ai/cron-next@latest`). No package
manager has it, so the README must not say that one does (not Homebrew, not apt, not npm, not pip).

"Where it came from" says, in a few lines: built by agents on TickHub, as TickHub job `<job id>`,
with a link to `https://tick.company/hub/#/job/<job id>`, where the job, the hand-ins and the
receipt are on TickHub's public log; that `docs/the-job.md` is the job as it was given; and the
line "AI does the work. Tick makes it trustworthy. https://tick.company". The job id is given to
the developer with the job.

"Licence" says: Apache-2.0, Copyright 2026 Parsa Esmaeili, see `LICENSE`.

## How it is run

    cron-next <command> <expression> [options]

Commands: `next`, `check`, `fields`. Each takes **exactly one** expression argument: the whole
expression as one argument (`"0 9 * * 1-5"`, quoted), or one macro (`@daily`). Options:

| option | for | meaning |
|---|---|---|
| `--count <n>` | `next` | how many fire times to print: a whole number from 1 to 1000, written with digits only, no sign and no leading zero. Default 5. |
| `--from <time>` | `next` | the moment the search starts after (see "The `--from` time"). Default: the current time. |
| `--tz <zone>` | `next` | the IANA time zone the schedule is read in and the times are printed in. Default `UTC`. |
| `--json` | all three | print one JSON object instead of the plain form. May be given more than once. |

Parsing, left to right: an argument that starts with `-` is an option; `--count`, `--from` and
`--tz` take the **next argument** as their value, whatever it holds (it is never read as an option
or a command); every other argument is positional. The first positional argument is the command,
the second is the expression, and there must be no more. Options may stand anywhere, before the
command, between or after. Every option other than the four above is an unknown option, including
`-` alone, `--` and the form `--count=3`. An option of another command (for example `--tz` with
`check`) is a usage error, not ignored. A value option (`--count`, `--from`, `--tz`) given twice, or
without a value, is a usage error.

Two special forms: when the **only** argument is `--help` or `-h`, a usage text is printed on stdout
(its first line starts with `usage: cron-next`, and it names `next`, `check` and `fields`) and the
exit status is 0. When the only argument is `--version`, the line `cron-next 0.1.0` is printed on
stdout and the exit status is 0. In any other command line `--help`, `-h` and `--version` are
unknown options.

**Exit codes.** 0: done. 1: `check` found a valid expression that can never fire; `next` found no
fire time to print. 2: a usage error or an invalid expression. When the exit status is 2,
**nothing at all** is printed on stdout (everything is checked before the first byte is written),
and stderr holds a message whose first line starts with `cron-next: `. A **usage error** is: no
command; an unknown command; an unknown option; a missing, repeated or invalid option value
(`--count`, `--from`, `--tz`); the wrong number of positional arguments; an option that does not
belong to the command. The message names what is wrong: for an invalid expression, an invalid
`--from` and an unknown `--tz` it holds the argument's text as given. Beyond that the wording of
messages is free. The tool never reads standard input and writes no file.

## The expression

An expression is **five fields**, in this order: minute (0-59), hour (0-23), day of month (1-31),
month (1-12) and day of week (0-7), **or one macro**. Fields are separated by one or more spaces or
tabs (U+0020, U+0009, nothing else); spaces and tabs at the start and the end are ignored. An empty
expression, or one with any other number of fields (4, 6, ...), is invalid.

A field is a **list**: one or more **items** separated by `,`. An empty item (`1,,2`, `,1`, `1,`)
is invalid. An item is one of:

* `*`: every value of the field, from its lowest to its highest (for the day of week 0 to 7).
* a value `a`;
* a range `a-b`: the values from `a` to `b`, both included; `a` must not be greater than `b`
  (there is **no wrap-around**: `22-2` and `FRI-SUN` are invalid), and both must lie in the field's
  bounds;
* a step `*/n` or `a-b/n`: starting at the first value (`a`, or the field's lowest value for `*`),
  every n-th value while it is not greater than the last (`b`, or the field's highest value for
  `*`). `n` is a number (one or two digits, see below) of 1 or more and **at most the highest value of the field** (59, 23,
  31, 12, 7): `*/0` and `*/60` in the minute field are invalid, `*/59` is `0,59`, `1-5/9` is `1`.
  A step on a single value (`5/10`) is invalid; a step belongs to one item, so `1,*/20` is the
  list of `1` and `*/20`.

A **value** is a number or a name. A **number** is one or two ASCII digits (`5` and `05` are fine,
`005` is not; no sign, no dot). A **name** is exactly three ASCII letters in any case, only in the
month field (`JAN`, `Feb`, `dec`: 1 to 12) and the day-of-week field (`SUN`, `mon`, `Tue`, `WED`,
`thu`, `Fri`, `sat`: 0 to 6; `SUN` is 0). Names and numbers may be mixed in a range (`MON-5`); a name
is never a step. Full names (`MONDAY`, `JANUARY`) are invalid. Bounds: a value outside the field's
bounds (`60` for the minute, `0` for the day of month, `13` for the month, `8` for the day of week)
is invalid.

**Sunday is 0 and 7.** In the day-of-week field `7` means Sunday, like `0`: `5-7` is Friday, Saturday
and Sunday, `0-7` and `*` are every day, and the expanded set holds only 0 to 6 (see `fields`).

**Nothing else is accepted**: no seconds field, no year field, no `?`, no `L`, no `W`, no `#`, no
names for hours or minutes, no `H`, no `@reboot`.

**Macros.** An expression that is one word starting with `@` is a macro (in any letter case); it
means:

| macro | means |
|---|---|
| `@yearly`, `@annually` | `0 0 1 1 *` |
| `@monthly` | `0 0 1 * *` |
| `@weekly` | `0 0 * * 0` |
| `@daily`, `@midnight` | `0 0 * * *` |
| `@hourly` | `0 * * * *` |

Any other word starting with `@` is invalid, and so is anything after a macro (`@daily extra`). A
single word that does not start with `@` is invalid.

### Which days match: the classic rule

A field's set is the set of values its list gives. A day-of-month or day-of-week field is
**starred** when its text (the field as written, before any expansion) **starts with the character
`*`**: `*`, `*/2` and `*,5` are starred; `1-31`, `0-7` and `MON-FRI` are not (they are
*restricted*). A calendar day (in the zone's local calendar) matches like this:

1. its month must be in the month set; and then
2. if **neither** the day-of-month field **nor** the day-of-week field is starred (both are
   restricted), the day matches when its day of month is in the day-of-month set **or** its weekday
   is in the day-of-week set: either one is enough (`0 0 15 * 1` fires on the 15th and on every
   Monday; `0 0 1-31 * 1` fires every day);
3. otherwise (at least one is starred) the day matches when its day of month is in the day-of-month
   set **and** its weekday is in the day-of-week set (`0 0 */2 * 1` fires on Mondays that fall on
   an odd day of the month, because `*/2` is 1, 3, ..., 31; `0 0 * * 1` fires on Mondays; `0 0 13 * *`
   on the 13th).

The calendar is the proleptic Gregorian calendar; the weekday of a date is its real weekday (Sunday
is 0). The day-of-month set may hold days a month does not have (31 in April): those simply never
match.

## Time zones and daylight saving

**The zone.** `--tz` takes a name of the IANA time zone database, exactly as the database spells it
(case matters), read from Go's embedded database (`time/tzdata`): `UTC`, `Europe/Berlin`,
`America/New_York`, `Asia/Kolkata`. The name is accepted when `time.LoadLocation` accepts it, **except** that the empty string and
`Local` are not accepted (the machine's own zone is never used). Any other name, and any with a
leading `/` or a `..`, is a usage error holding the name. The default is `UTC`.

**A fire time.** An instant (a point in time) is a **fire time** when its wall-clock reading in the
zone has the second 0 and the schedule matches that reading: the reading's minute is in the minute
set, its hour in the hour set, and its calendar day matches by the rules above. An instant at which
the zone's offset from UTC is not a whole number of minutes is never a fire time. The rule is
about wall-clock readings and nothing else, so daylight saving needs no special case:

* **A local time that does not exist never fires.** When the clocks jump forward (in
  `America/New_York` from 02:00 to 03:00 on 2026-03-08), no instant reads 02:30 that day, so
  `30 2 * * *` does not fire that day at all. Nothing fires later to make up for it, and nothing
  else changes: `0 * * * *` fires at 01:00 and then at 03:00.
* **A local time that happens twice fires twice.** When the clocks go back (in `Europe/Berlin` from
  03:00 to 02:00 on 2026-10-25), the readings from 02:00 to 02:59 occur twice, first with the
  summer offset and then with the winter offset, and `30 2 * * *` fires at both instants. A reading
  that occurs only once fires once (03:00 that day is the winter reading only).
* A change that is not a whole hour is handled by the same rule (`Australia/Lord_Howe` moves by
  30 minutes: from 02:00 to 02:30 on 2026-10-04, and from 02:00 back to 01:30 on 2026-04-05).

All fire times are distinct instants, printed in ascending order of the instant.

## The `--from` time

`--from` is an RFC 3339 time in exactly this form: `YYYY-MM-DDTHH:MM:SS`, then optionally `.` and 1
to 9 digits, then `Z` or an offset `+HH:MM` or `-HH:MM`. The `T` and `Z` are capital letters; the
seconds are required; the date and time must be real (month 1-12, a day that exists, hour 0-23,
minute 0-59, second 0-59: `:60` is invalid, and so is `24:00:00`); the offset's hours are 00-23 and
its minutes 00-59. Any other text is a usage error. The time is an **instant**: its offset only says
which instant it is and does not choose the zone (`--tz` does); `2026-01-01T05:30:00+05:30` and
`2026-01-01T00:00:00Z` are the same instant. The instant must lie from `1970-01-01T00:00:00Z` to
`2100-12-31T23:59:59Z`, both included; otherwise it is a usage error. Without `--from` the
instant is the current time (and is not stated further, so a run without it depends on the clock).

**The `--from` instant itself is never an answer**: the answers are the fire times that are
**strictly after** it. A `--from` with seconds or a fraction is therefore fine: with
`* * * * *` and `--from 2026-01-01T00:00:30Z` the first answer is 00:01:00, and with
`--from 2026-01-01T00:00:00Z` it is also 00:01:00, because 00:00:00 is not after itself. A fraction
counts as time after: `00:00:00.5Z` is after `00:00:00Z`.

**How the search ends.** The Gregorian calendar repeats exactly every 400 years (146097 days, a
whole number of weeks), so every command ends: `next` looks at the fire times **t with
`from < t <= from + 146097 days`** only, where `from + 146097 days` is exactly 146097 × 86400
seconds after the `--from` instant (the upper bound is included). It prints the first `--count`
of them, or all of them when there are fewer: fewer lines than `--count` is not an error. (From
`2026-01-01T00:00:00Z`, `0 0 29 2 *` has 97 fire times, the last on 2424-02-29, because 2100, 2200
and 2300 are not leap years and 2400 is.) `check` does not look at time: it asks whether any
calendar date matches (below).

## Commands

### `next <expression>`

Prints the fire times, one per line, in ascending order: at most `--count`, and none after the
search window. Each line is `YYYY-MM-DDTHH:MM:SS±HH:MM`: the instant's wall-clock reading in the zone,
with the second always `00`, then the zone's offset at that instant as a sign and two digits of
hours and two of minutes. **The offset is always numeric: a zero offset is `+00:00`, never `Z`**
(so UTC prints `2026-01-01T09:00:00+00:00`, `Asia/Kolkata` prints `+05:30`, `Australia/Lord_Howe`
`+10:30` or `+11:00`). Each line ends with `\n` and nothing else is printed.

`--json`: one object and a newline, with the keys `expression` (string: the expression argument
exactly as given), `timezone` (string: the zone name as given, `UTC` by default), `from` (string:
the `--from` instant in the same form as the times, in the zone, with the fraction of a second cut
off) and `times` (an array of strings, each the same text as one plain line, in the same order).

**When there is no fire time to print** (the schedule never fires, so none is in the search
window): the plain form prints **nothing** (zero bytes) and the exit status is 1; with `--json` the
object is printed with `"times": []` and the exit status is 1. When at least one is printed the exit
status is 0.

### `check <expression>`

Tells whether the expression is valid, and whether it can ever fire. **It judges the calendar only,
not a time zone**: the expression **can fire** when at least one calendar date of the proleptic
Gregorian calendar matches it by the day rules above (the minute and hour sets are never empty).
Since the calendar repeats every 400 years it is enough to look at 400 years. So `0 0 31 2 *`,
`0 0 30 2 *`, `0 0 31 4,6,9,11 *` and `0 0 30 2 */2` never fire, `0 0 29 2 *` fires (in leap years),
`0 0 31 2 1` fires (every Monday of February, because both day fields are restricted) and
`0 0 30 2 0-6` fires (every day of February).

The plain form is one line: `valid: can fire` (exit 0), or `valid: never fires` (exit 1). `--json`:
one object with the keys `expression` (string, as given), `valid` (always `true`: an invalid
expression is exit 2 with nothing on stdout) and `fires` (boolean); the exit status is the same as
in the plain form.

### `fields <expression>`

Shows what the expression really means: the **expanded values** of every field, each set in
ascending order without duplicates, as decimal numbers (names become numbers, a range and a step
become their values, `*` becomes every value, the day of week 7 becomes 0). The plain form is
exactly these six lines, in this order, each `key: value`, one space after the colon, values
separated by single spaces, a final newline:

    minute: 0 15 30 45
    hour: 9 10 11 12 13 14 15 16 17
    day_of_month: 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30 31
    month: 1 2 3 4 5 6 7 8 9 10 11 12
    day_of_week: 1 2 3 4 5
    day_rule: and

(that is `fields '*/15 9-17 * * 1-5'`). `day_of_month` of `*` is 1 to 31, `day_of_week` of `*` is
0 to 6. `day_rule` is `or` when neither the day-of-month nor the day-of-week field is starred (a
day matches when either set holds it), and `and` otherwise (a day matches when both do). A macro
shows the fields it stands for. `--json`: one object with the keys `expression` (string, as given),
`minute`, `hour`, `day_of_month`, `month`, `day_of_week` (arrays of numbers, in ascending order) and
`day_rule` (the string `or` or `and`). The exit status is 0 for a valid expression, even one that
can never fire.

## What is printed in the empty cases

* `next` with no fire time: plain, zero bytes on stdout, exit 1; `--json`, the object with `"times": []`, exit 1.
* `check`, `fields`: always print at least one line (or the object) when the expression is valid.
* On every exit status 2 stdout holds nothing.

## Output and JSON in general

In `--json` stdout is one JSON object and a newline, with the keys named above and no others (their
order and the spacing are free); text is encoded as JSON requires, numbers are JSON numbers. In the
plain form lines end with `\n` and nothing else is printed. stderr is for messages only and is never
read for more than the words stated above.

## Your tests, and the build

`main_test.go` holds your tests of the rules above (at least: the expansion of ranges, steps, names
and 7; the day rule with a starred and a restricted field; invalid expressions; a daylight-saving
gap and a repeated hour; the strict start after `--from`; the 400-year window). They run with
`go test ./...` without network, finish in seconds, and pass.

## What the check looks at

The work is built with `go build`, and cron-next is run as a child process with arguments, from a
folder of its own; only what it prints on stdout, its exit status and (for the exit status 2) the
words stated above on stderr are looked at. Every run passes `--from`, so no answer depends on the
clock. Daylight saving is tested with `America/New_York`, `Europe/Berlin` and `Australia/Lord_Howe`
in the years 2026 and 2027. The check also runs `go vet .`, `gofmt -l` on the `.go` files at the top
of the work and `go test .`.
