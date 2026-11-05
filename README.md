# cron-next

**cron-next tells you exactly when a cron schedule fires in your time zone, whether it can ever fire, and what every field expands to.**

    cron-next <command> <expression> [options]

```
$ cron-next next "30 2 * * *" --tz Europe/Berlin --from 2026-10-24T12:00:00Z --count 3
2026-10-25T02:30:00+02:00
2026-10-25T02:30:00+01:00
2026-10-26T02:30:00+01:00

$ cron-next check "0 0 31 2 *"
valid: never fires
$ echo $?
1

$ cron-next fields "*/15 9-17 * * 1-5"
minute: 0 15 30 45
hour: 9 10 11 12 13 14 15 16 17
day_of_month: 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30 31
month: 1 2 3 4 5 6 7 8 9 10 11 12
day_of_week: 1 2 3 4 5
day_rule: and
```

(The first example is the night the clocks go back in Berlin: 02:30 happens twice, and the schedule fires twice.)

## Why it matters

Agents write CI schedules, Kubernetes CronJobs and crontab lines, and a reviewer has to work out in
their head when `*/15 9-17 * * 1-5` fires, whether `0 0 31 2 *` ever does, and what happens on the
night the clocks change. cron-next is one static binary, Go standard library only, that answers
exactly. Every field is read by one stated rule, time zones and daylight saving are handled as
stated, and nothing is guessed: an expression that is not exactly valid is an error, never repaired.

## Build and run

You need Go 1.27 or later. From a clone of github.com/tick-ai/cron-next:

    go build -o cron-next .

or

    go install github.com/tick-ai/cron-next@latest

There is no other way to get it today: no package manager has it. The binary embeds Go's time zone
database (`time/tzdata`), so it needs no zone files on the machine, no network, and it never reads
standard input or writes a file.

    cron-next <command> <expression> [options]

Commands: `next`, `check`, `fields`. Each takes **exactly one** expression argument: the whole
expression as one argument (`"0 9 * * 1-5"`, quoted), or one macro (`@daily`).

| option | for | meaning |
|---|---|---|
| `--count <n>` | `next` | how many fire times to print: a whole number from 1 to 1000, digits only, no sign and no leading zero. Default 5. |
| `--from <time>` | `next` | the moment the search starts after (see "The `--from` time"). Default: the current time. |
| `--tz <zone>` | `next` | the IANA time zone the schedule is read in and the times are printed in. Default `UTC`. |
| `--json` | all three | print one JSON object instead of the plain form. May be given more than once. |

Arguments are read left to right. An argument that starts with `-` is an option; `--count`, `--from`
and `--tz` take the **next argument** as their value, whatever it holds. Every other argument is
positional: the first is the command, the second the expression, and there must be no more. Options
may stand anywhere. Every other option is unknown, including `-` alone, `--` and `--count=3`. An
option of another command (`--tz` with `check`) is a usage error, not ignored; so is a value option
given twice or without a value. When the **only** argument is `--help` or `-h`, a usage text is
printed (exit 0); when it is `--version`, `cron-next 0.1.0` is printed (exit 0). In any other command
line they are unknown options.

## Commands

### `next <expression>`

Prints the fire times strictly after `--from`, one per line, in ascending order: at most `--count`,
and none after the search window.

```
$ cron-next next "0 9 * * 1-5" --tz Asia/Kolkata --from 2026-01-01T00:00:00Z --count 3
2026-01-01T09:00:00+05:30
2026-01-02T09:00:00+05:30
2026-01-05T09:00:00+05:30
```

Each line is `YYYY-MM-DDTHH:MM:SS±HH:MM`: the wall-clock reading in the zone (second always `00`) and the
zone's offset at that instant. The offset is always numeric: a zero offset is `+00:00`, never `Z`.
When no fire time lies in the search window, nothing is printed and the exit status is 1.

### `check <expression>`

Tells whether any calendar date can match. It judges the calendar only, not a time zone.

```
$ cron-next check "0 0 29 2 *"
valid: can fire
$ cron-next check "0 0 30 2 *"
valid: never fires
```

`valid: can fire` is exit 0, `valid: never fires` is exit 1. `0 0 31 2 1` can fire (both day fields are
restricted, so every Monday of February matches); `0 0 30 2 */2` cannot.

### `fields <expression>`

Shows the expanded values of every field, each set ascending without duplicates, names turned into
numbers and 7 into 0, followed by the day rule (`or` or `and`). See the example above. A macro shows the
fields it stands for. Exit status 0 for any valid expression, even one that can never fire.

## The dialect

An expression is **five fields**: minute (0-59), hour (0-23), day of month (1-31), month (1-12) and day
of week (0-7), **or one macro**. Fields are separated by one or more spaces or tabs (nothing else);
spaces and tabs at the ends are ignored.

A field is a list of one or more items separated by `,`; an empty item (`1,,2`, `,1`, `1,`) is invalid.
An item is:

* `*`: every value of the field (for the day of week 0 to 7);
* a value `a`;
* a range `a-b`, both included, `a` not greater than `b`: there is **no wrap-around** (`22-2` and
  `FRI-SUN` are invalid);
* a step `*/n` or `a-b/n`: from the first value, every n-th value while it does not pass the last.
  `n` is 1 or more, one or two digits, and at most the field's highest value (59, 23, 31, 12, 7):
  `*/60` in the minute field is invalid, `*/59` is `0,59`, `1-5/9` is `1`. A step on a single value
  (`5/10`) is invalid.

A value is a number (one or two ASCII digits: `05` is fine, `005` is not; no sign) or a three-letter
name in any case, only in the month field (`JAN` to `DEC`) and the day-of-week field (`SUN` to `SAT`).
Names and numbers may be mixed in a range (`MON-5`); a name is never a step. Full names (`MONDAY`) are
invalid. **Sunday is 0 and 7**: `5-7` is Friday, Saturday and Sunday, and the expanded set holds only 0
to 6.

**Nothing else is accepted**: no seconds field, no year, no `?`, `L`, `W`, `#`, no `H`, no `@reboot`.

| macro | means |
|---|---|
| `@yearly`, `@annually` | `0 0 1 1 *` |
| `@monthly` | `0 0 1 * *` |
| `@weekly` | `0 0 * * 0` |
| `@daily`, `@midnight` | `0 0 * * *` |
| `@hourly` | `0 * * * *` |

Macros may be written in any letter case. Any other word starting with `@` is invalid, and so is
anything after a macro (`@daily extra`).

## The day rule

A day-of-month or day-of-week field is **starred** when its text, as written, starts with `*`
(`*`, `*/2` and `*,5` are starred; `1-31`, `0-7` and `MON-FRI` are restricted). A calendar day, in the
zone's local calendar, matches like this:

1. its month must be in the month set; and then
2. if **both** day fields are restricted, the day matches when its day of month is in the
   day-of-month set **or** its weekday is in the day-of-week set (`0 0 15 * 1` fires on the 15th and on
   every Monday; `0 0 1-31 * 1` fires every day);
3. otherwise the day matches when its day of month is in the set **and** its weekday is in the set
   (`0 0 */2 * 1` fires on Mondays that fall on an odd day of the month; `0 0 * * 1` on Mondays;
   `0 0 13 * *` on the 13th).

The calendar is the proleptic Gregorian calendar. Days a month does not have (31 in April) never match.

## Time zones and daylight saving

`--tz` takes a name of the IANA time zone database exactly as it is spelled (case matters), read from
Go's embedded database: `UTC`, `Europe/Berlin`, `America/New_York`, `Asia/Kolkata`. The empty string
and `Local` are not accepted (the machine's own zone is never used), nor is a name with a leading `/` or
a `..`. The default is `UTC`.

An instant is a **fire time** when its wall-clock reading in the zone has the second 0 and the
schedule matches that reading. An instant at which the zone's offset is not a whole number of minutes
is never a fire time. Daylight saving needs no special case:

* **A local time that does not exist never fires.** In `America/New_York` the clocks jump from 02:00 to
  03:00 on 2026-03-08, so `30 2 * * *` does not fire that day, and nothing fires later to make up for
  it. `0 * * * *` fires at 01:00 and then at 03:00.
* **A local time that happens twice fires twice.** In `Europe/Berlin` the clocks go back from 03:00 to
  02:00 on 2026-10-25, so `30 2 * * *` fires at both 02:30 instants. A reading that occurs once fires
  once (03:00 that day is the winter reading only).
* A change that is not a whole hour follows the same rule (`Australia/Lord_Howe` moves by 30 minutes).

All fire times are distinct instants, printed in ascending order of the instant.

## The `--from` time

`--from` is an RFC 3339 time in exactly this form: `YYYY-MM-DDTHH:MM:SS`, optionally `.` and 1 to 9
digits, then `Z` or `+HH:MM` / `-HH:MM`. `T` and `Z` are capitals; the seconds are required; the date and
time must be real (`:60` and `24:00:00` are invalid); the offset's hours are 00-23, its minutes 00-59.
The time is an **instant**: its offset only says which instant it is and does not choose the zone
(`--tz` does). It must lie from `1970-01-01T00:00:00Z` to `2100-12-31T23:59:59Z`, both included.
Without `--from` it is the current time.

**The `--from` instant itself is never an answer**: the answers are strictly after it. With `* * * * *`
and `--from 2026-01-01T00:00:00Z` (or `…00:00:30Z`) the first answer is 00:01:00. A fraction counts as
time after: `00:00:00.5Z` is after `00:00:00Z`.

## Limits

The Gregorian calendar repeats exactly every 400 years (146097 days, a whole number of weeks), so every
command ends. `next` looks only at fire times `t` with `from < t <= from + 146097 days` (exactly
146097 × 86400 seconds, upper bound included) and prints the first `--count` of them, or all when there
are fewer: that is not an error. From `2026-01-01T00:00:00Z`, `0 0 29 2 *` has 97 fire times, the last
on 2424-02-29, because 2100, 2200 and 2300 are not leap years and 2400 is. `--count` is at most 1000.
`check` does not look at time: it asks whether any calendar date within a 400-year cycle matches.

## Exit codes

| code | meaning |
|---|---|
| 0 | done |
| 1 | `check` found a valid expression that can never fire; `next` found no fire time to print |
| 2 | a usage error or an invalid expression |

On exit status 2 **nothing at all** is printed on stdout (everything is checked before the first byte is
written), and stderr holds a message whose first line starts with `cron-next: `. For an invalid
expression, an invalid `--from` and an unknown `--tz` the message holds the argument's text as given.

## JSON forms

`--json` prints one object and a newline.

`next` (an empty `times` is exit 1):

```json
{"expression":"0 9 * * 1-5","timezone":"Asia/Kolkata","from":"2026-01-01T05:30:00+05:30","times":["2026-01-01T09:00:00+05:30"]}
```

`expression` is the argument as given, `timezone` the zone name as given, `from` the `--from` instant in
the zone with the fraction cut off, `times` the same strings as the plain lines.

`check`:

```json
{"expression":"0 0 31 2 *","valid":true,"fires":false}
```

`fields`:

```json
{"expression":"@hourly","minute":[0],"hour":[0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23],"day_of_month":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31],"month":[1,2,3,4,5,6,7,8,9,10,11,12],"day_of_week":[0,1,2,3,4,5,6],"day_rule":"and"}
```

## Where it came from

Built by agents on TickHub, as TickHub job `29beaeee-ab18-4afd-9a93-b3960c4da137`. The job, the
hand-ins and the receipt are on TickHub's public log:
https://tick.company/hub/#/job/29beaeee-ab18-4afd-9a93-b3960c4da137

`docs/the-job.md` is the job as it was given.

AI does the work. Tick makes it trustworthy. https://tick.company

## Licence

Apache-2.0, Copyright 2026 Parsa Esmaeili, see `LICENSE`.
