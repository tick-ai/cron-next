// Command cron-next tells when a cron schedule really fires, in a time zone,
// exactly: the next fire times, whether it can ever fire, and what every field
// expands to.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

const version = "0.1.0"

// windowSeconds is the length of the search window of next: 146097 days, the
// exact length of the 400-year Gregorian cycle.
const windowSeconds = 146097 * 86400

const timeLayout = "2006-01-02T15:04:05-07:00"

const usageText = `usage: cron-next <command> <expression> [options]

Commands (each takes exactly one expression: five fields, quoted, or one macro):
  next <expression>     print the next fire times, one per line
  check <expression>    tell whether any calendar date can match (exit 1 if never)
  fields <expression>   show what every field expands to

Options:
  --count <n>   next only: how many times, 1 to 1000 (default 5)
  --from <t>    next only: RFC 3339 instant the search starts after (default: now)
  --tz <zone>   next only: IANA time zone, e.g. Europe/Berlin (default UTC)
  --json        print one JSON object instead of the plain form
  -h, --help    print this text (alone)
  --version     print the version (alone)

Exit status: 0 done; 1 never fires / nothing to print; 2 usage error or invalid expression.
`

// ---------------------------------------------------------------------------
// Expressions

type fieldSpec struct {
	name     string
	lo, hi   int
	names    []string
	nameBase int
}

var monthNames = []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}
var dowNames = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

var specs = [5]fieldSpec{
	{"minute", 0, 59, nil, 0},
	{"hour", 0, 23, nil, 0},
	{"day of month", 1, 31, nil, 0},
	{"month", 1, 12, monthNames, 1},
	{"day of week", 0, 7, dowNames, 0},
}

var macros = map[string]string{
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
	"@monthly":  "0 0 1 * *",
	"@weekly":   "0 0 * * 0",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@hourly":   "0 * * * *",
}

// schedule is a parsed expression.
type schedule struct {
	minute, hour, dom, month, dow []bool // indexed by value (dow: 0..6)
	domStar, dowStar              bool
}

// orRule reports whether a day matches on day of month or day of week.
func (s *schedule) orRule() bool { return !s.domStar && !s.dowStar }

func (s *schedule) dayMatches(month, dom, dow int) bool {
	if !s.month[month] {
		return false
	}
	if s.orRule() {
		return s.dom[dom] || s.dow[dow]
	}
	return s.dom[dom] && s.dow[dow]
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

func isBlank(c byte) bool { return c == ' ' || c == '\t' }

// splitBlanks splits on runs of spaces and tabs, ignoring them at both ends.
func splitBlanks(s string) []string {
	var out []string
	i := 0
	for i < len(s) {
		for i < len(s) && isBlank(s[i]) {
			i++
		}
		if i >= len(s) {
			break
		}
		j := i
		for j < len(s) && !isBlank(s[j]) {
			j++
		}
		out = append(out, s[i:j])
		i = j
	}
	return out
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isLetters(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i] | 0x20
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}

// parseNumber reads one or two ASCII digits.
func parseNumber(s string) (int, bool) {
	if !isDigits(s) || len(s) > 2 {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

func parseValue(tok string, sp fieldSpec) (int, error) {
	var v int
	switch {
	case isDigits(tok):
		n, ok := parseNumber(tok)
		if !ok {
			return 0, fmt.Errorf("%s: %q is not a one or two digit number", sp.name, tok)
		}
		v = n
	case sp.names != nil && len(tok) == 3 && isLetters(tok):
		l := asciiLower(tok)
		found := false
		for i, n := range sp.names {
			if n == l {
				v, found = sp.nameBase+i, true
				break
			}
		}
		if !found {
			return 0, fmt.Errorf("%s: unknown name %q", sp.name, tok)
		}
	default:
		return 0, fmt.Errorf("%s: %q is not a value", sp.name, tok)
	}
	if v < sp.lo || v > sp.hi {
		return 0, fmt.Errorf("%s: %q is outside %d-%d", sp.name, tok, sp.lo, sp.hi)
	}
	return v, nil
}

func parseField(text string, sp fieldSpec) ([]bool, error) {
	size := sp.hi + 1
	if sp.hi == 7 {
		size = 7
	}
	set := make([]bool, size)
	for _, item := range strings.Split(text, ",") {
		if item == "" {
			return nil, fmt.Errorf("%s: empty list item", sp.name)
		}
		base, stepText, hasStep := strings.Cut(item, "/")
		var a, b int
		if base == "*" {
			a, b = sp.lo, sp.hi
		} else if left, right, isRange := strings.Cut(base, "-"); isRange {
			var err error
			if a, err = parseValue(left, sp); err != nil {
				return nil, err
			}
			if b, err = parseValue(right, sp); err != nil {
				return nil, err
			}
			if a > b {
				return nil, fmt.Errorf("%s: range %q runs backwards", sp.name, base)
			}
		} else {
			if hasStep {
				return nil, fmt.Errorf("%s: a step needs * or a range: %q", sp.name, item)
			}
			v, err := parseValue(base, sp)
			if err != nil {
				return nil, err
			}
			a, b = v, v
		}
		step := 1
		if hasStep {
			n, ok := parseNumber(stepText)
			if !ok || n < 1 || n > sp.hi {
				return nil, fmt.Errorf("%s: invalid step %q (1 to %d)", sp.name, stepText, sp.hi)
			}
			step = n
		}
		for v := a; v <= b; v += step {
			if sp.hi == 7 && v == 7 {
				set[0] = true // Sunday is 0 and 7
				continue
			}
			set[v] = true
		}
	}
	return set, nil
}

func parseExpression(expr string) (*schedule, error) {
	words := splitBlanks(expr)
	if len(words) == 0 {
		return nil, fmt.Errorf("empty expression")
	}
	if len(words) == 1 {
		if !strings.HasPrefix(words[0], "@") {
			return nil, fmt.Errorf("expected five fields or a macro, got one word")
		}
		m, ok := macros[asciiLower(words[0])]
		if !ok {
			return nil, fmt.Errorf("unknown macro %q", words[0])
		}
		words = strings.Fields(m)
	}
	if len(words) != 5 {
		return nil, fmt.Errorf("expected 5 fields, got %d", len(words))
	}
	s := &schedule{}
	dst := []*[]bool{&s.minute, &s.hour, &s.dom, &s.month, &s.dow}
	for i, w := range words {
		set, err := parseField(w, specs[i])
		if err != nil {
			return nil, err
		}
		*dst[i] = set
	}
	s.domStar = strings.HasPrefix(words[2], "*")
	s.dowStar = strings.HasPrefix(words[4], "*")
	return s, nil
}

func values(set []bool) []int {
	out := []int{}
	for v, ok := range set {
		if ok {
			out = append(out, v)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Options

type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, a ...any) error { return &usageError{fmt.Sprintf(format, a...)} }

func parseCount(s string) (int, error) {
	if !isDigits(s) || s[0] == '0' || len(s) > 4 {
		return 0, usagef("invalid --count '%s': a whole number from 1 to 1000, digits only", s)
	}
	n, _ := strconv.Atoi(s)
	if n > 1000 {
		return 0, usagef("invalid --count '%s': a whole number from 1 to 1000, digits only", s)
	}
	return n, nil
}

func digitsAt(s string, from, n int) (int, bool) {
	if from+n > len(s) {
		return 0, false
	}
	v := 0
	for i := from; i < from+n; i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		v = v*10 + int(s[i]-'0')
	}
	return v, true
}

func daysIn(y, m int) int {
	switch m {
	case 2:
		if y%4 == 0 && (y%100 != 0 || y%400 == 0) {
			return 29
		}
		return 28
	case 4, 6, 9, 11:
		return 30
	}
	return 31
}

var maxFrom = time.Date(2100, 12, 31, 23, 59, 59, 0, time.UTC).Unix()

// parseFrom reads the --from time: the instant as whole seconds since the
// epoch, and the nanoseconds after them.
func parseFrom(s string) (int64, int, error) {
	bad := func() (int64, int, error) {
		return 0, 0, usagef("invalid --from '%s': want YYYY-MM-DDTHH:MM:SS[.fraction](Z|+HH:MM|-HH:MM), 1970-01-01T00:00:00Z to 2100-12-31T23:59:59Z", s)
	}
	if len(s) < 20 || s[4] != '-' || s[7] != '-' || s[10] != 'T' || s[13] != ':' || s[16] != ':' {
		return bad()
	}
	y, ok1 := digitsAt(s, 0, 4)
	mo, ok2 := digitsAt(s, 5, 2)
	d, ok3 := digitsAt(s, 8, 2)
	h, ok4 := digitsAt(s, 11, 2)
	mi, ok5 := digitsAt(s, 14, 2)
	sec, ok6 := digitsAt(s, 17, 2)
	if !(ok1 && ok2 && ok3 && ok4 && ok5 && ok6) {
		return bad()
	}
	if mo < 1 || mo > 12 || d < 1 || d > daysIn(y, mo) || h > 23 || mi > 59 || sec > 59 {
		return bad()
	}
	rest := s[19:]
	nsec := 0
	if rest[0] == '.' {
		n := 1
		for n < len(rest) && rest[n] >= '0' && rest[n] <= '9' {
			n++
		}
		digits := rest[1:n]
		if len(digits) < 1 || len(digits) > 9 {
			return bad()
		}
		nsec, _ = strconv.Atoi(digits + strings.Repeat("0", 9-len(digits)))
		rest = rest[n:]
	}
	offset := 0
	switch {
	case rest == "Z":
	case len(rest) == 6 && (rest[0] == '+' || rest[0] == '-') && rest[3] == ':':
		oh, okh := digitsAt(rest, 1, 2)
		om, okm := digitsAt(rest, 4, 2)
		if !okh || !okm || oh > 23 || om > 59 {
			return bad()
		}
		offset = oh*3600 + om*60
		if rest[0] == '-' {
			offset = -offset
		}
	default:
		return bad()
	}
	t := time.Date(y, time.Month(mo), d, h, mi, sec, 0, time.UTC).Unix() - int64(offset)
	if t < 0 || t > maxFrom || (t == maxFrom && nsec != 0) {
		return bad()
	}
	return t, nsec, nil
}

func parseZone(name string) (*time.Location, error) {
	if name == "" || name == "Local" || strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
		return nil, usagef("unknown --tz '%s'", name)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, usagef("unknown --tz '%s'", name)
	}
	return loc, nil
}

// ---------------------------------------------------------------------------
// Computing

// canFire reports whether any calendar date matches, looking at one whole
// 400-year cycle.
func (s *schedule) canFire() bool {
	start := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).Unix() / 86400
	for day := start; day < start+146097; day++ {
		t := time.Unix(day*86400, 0).UTC()
		_, m, d := t.Date()
		if s.dayMatches(int(m), d, int(t.Weekday())) {
			return true
		}
	}
	return false
}

func localDay(sec int64, loc *time.Location) int64 {
	y, m, d := time.Unix(sec, 0).In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400
}

func offsetAt(sec int64, loc *time.Location) int {
	_, off := time.Unix(sec, 0).In(loc).Zone()
	return off
}

// fireTimes returns up to count fire times t, as seconds since the epoch, with
// from < t <= from + 146097 days, in ascending order.
func (s *schedule) fireTimes(loc *time.Location, from int64, count int) []int64 {
	end := from + windowSeconds
	firstDay := localDay(from, loc) - 1
	lastDay := localDay(end, loc) + 1
	minutes, hours := values(s.minute), values(s.hour)
	var res []int64
	extra := -1
	for day := firstDay; day <= lastDay; day++ {
		if extra >= 0 {
			// Enough found; look at two more days so that the order of
			// instants across neighbouring calendar days cannot matter.
			if extra++; extra > 2 {
				break
			}
		}
		w0 := day * 86400
		t := time.Unix(w0, 0).UTC()
		_, m, d := t.Date()
		if !s.dayMatches(int(m), d, int(t.Weekday())) {
			continue
		}
		// The offsets in force around this local day.
		var offs []int
		for k := int64(0); k < 8; k++ {
			o := offsetAt(w0-16*3600+k*8*3600, loc)
			dup := false
			for _, p := range offs {
				if p == o {
					dup = true
				}
			}
			if !dup && o%60 == 0 {
				offs = append(offs, o)
			}
		}
		for _, h := range hours {
			for _, mi := range minutes {
				w := w0 + int64(h)*3600 + int64(mi)*60
				for _, o := range offs {
					u := w - int64(o)
					if u > from && u <= end && offsetAt(u, loc) == o {
						res = append(res, u)
					}
				}
			}
		}
		if len(res) >= count && extra < 0 {
			extra = 0
		}
	}
	sort.Slice(res, func(i, j int) bool { return res[i] < res[j] })
	out := res[:0]
	for i, u := range res {
		if i > 0 && u == res[i-1] {
			continue
		}
		out = append(out, u)
	}
	if len(out) > count {
		out = out[:count]
	}
	return out
}

func formatTime(sec int64, loc *time.Location) string {
	return time.Unix(sec, 0).In(loc).Format(timeLayout)
}

// ---------------------------------------------------------------------------
// Command line

func writeJSON(out *bytes.Buffer, v any) {
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func joinInts(v []int) string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, " ")
}

// run executes one command line and returns the exit status. Nothing is
// written to stdout unless everything has been checked.
func run(args []string, now time.Time, stdout, stderr io.Writer) int {
	if len(args) == 1 {
		switch args[0] {
		case "--help", "-h":
			io.WriteString(stdout, usageText)
			return 0
		case "--version":
			io.WriteString(stdout, "cron-next "+version+"\n")
			return 0
		}
	}
	code, out, err := execute(args, now)
	if err != nil {
		fmt.Fprintln(stderr, "cron-next: "+err.Error())
		return 2
	}
	stdout.Write(out)
	return code
}

func execute(args []string, now time.Time) (int, []byte, error) {
	var positional []string
	optVals := map[string]string{}
	jsonOut := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		switch a {
		case "--json":
			jsonOut = true
		case "--count", "--from", "--tz":
			if i+1 >= len(args) {
				return 0, nil, usagef("%s needs a value", a)
			}
			if _, dup := optVals[a]; dup {
				return 0, nil, usagef("%s given more than once", a)
			}
			optVals[a] = args[i+1]
			i++
		default:
			return 0, nil, usagef("unknown option '%s'", a)
		}
	}
	if len(positional) == 0 {
		return 0, nil, usagef("no command (next, check or fields); try --help")
	}
	command := positional[0]
	switch command {
	case "next", "check", "fields":
	default:
		return 0, nil, usagef("unknown command '%s' (next, check or fields)", command)
	}
	if command != "next" {
		for _, o := range []string{"--count", "--from", "--tz"} {
			if _, ok := optVals[o]; ok {
				return 0, nil, usagef("%s belongs to next, not %s", o, command)
			}
		}
	}
	if len(positional) < 2 {
		return 0, nil, usagef("%s needs one expression", command)
	}
	if len(positional) > 2 {
		return 0, nil, usagef("%s takes exactly one expression, got %d arguments (quote the expression)", command, len(positional)-1)
	}
	expr := positional[1]

	count := 5
	tzName := "UTC"
	loc := time.UTC
	fromSec, fromNsec := now.Unix(), 0
	if command == "next" {
		var err error
		if v, ok := optVals["--count"]; ok {
			if count, err = parseCount(v); err != nil {
				return 0, nil, err
			}
		}
		if v, ok := optVals["--from"]; ok {
			if fromSec, fromNsec, err = parseFrom(v); err != nil {
				return 0, nil, err
			}
		}
		if v, ok := optVals["--tz"]; ok {
			tzName = v
			if loc, err = parseZone(v); err != nil {
				return 0, nil, err
			}
		}
	}
	_ = fromNsec // a fraction counts as time after the whole second: no fire time lies between

	s, err := parseExpression(expr)
	if err != nil {
		return 0, nil, usagef("invalid expression '%s': %v", expr, err)
	}

	var out bytes.Buffer
	switch command {
	case "fields":
		rule := "and"
		if s.orRule() {
			rule = "or"
		}
		min, hour, dom, month, dow := values2(s)
		if jsonOut {
			writeJSON(&out, struct {
				Expression string `json:"expression"`
				Minute     []int  `json:"minute"`
				Hour       []int  `json:"hour"`
				DayOfMonth []int  `json:"day_of_month"`
				Month      []int  `json:"month"`
				DayOfWeek  []int  `json:"day_of_week"`
				DayRule    string `json:"day_rule"`
			}{expr, min, hour, dom, month, dow, rule})
		} else {
			fmt.Fprintf(&out, "minute: %s\nhour: %s\nday_of_month: %s\nmonth: %s\nday_of_week: %s\nday_rule: %s\n",
				joinInts(min), joinInts(hour), joinInts(dom), joinInts(month), joinInts(dow), rule)
		}
		return 0, out.Bytes(), nil
	case "check":
		fires := s.canFire()
		if jsonOut {
			writeJSON(&out, struct {
				Expression string `json:"expression"`
				Valid      bool   `json:"valid"`
				Fires      bool   `json:"fires"`
			}{expr, true, fires})
		} else if fires {
			out.WriteString("valid: can fire\n")
		} else {
			out.WriteString("valid: never fires\n")
		}
		if fires {
			return 0, out.Bytes(), nil
		}
		return 1, out.Bytes(), nil
	}
	// next
	secs := s.fireTimes(loc, fromSec, count)
	lines := make([]string, len(secs))
	for i, u := range secs {
		lines[i] = formatTime(u, loc)
	}
	if jsonOut {
		writeJSON(&out, struct {
			Expression string   `json:"expression"`
			Timezone   string   `json:"timezone"`
			From       string   `json:"from"`
			Times      []string `json:"times"`
		}{expr, tzName, formatTime(fromSec, loc), append([]string{}, lines...)})
	} else {
		for _, l := range lines {
			out.WriteString(l + "\n")
		}
	}
	if len(lines) == 0 {
		return 1, out.Bytes(), nil
	}
	return 0, out.Bytes(), nil
}

func values2(s *schedule) (min, hour, dom, month, dow []int) {
	return values(s.minute), values(s.hour), values(s.dom), values(s.month), values(s.dow)
}

func main() {
	os.Exit(run(os.Args[1:], time.Now(), os.Stdout, os.Stderr))
}
