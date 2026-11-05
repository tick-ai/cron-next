package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cron-next-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "cron-next")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build failed:", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type result struct {
	stdout, stderr string
	code           int
}

// runBin executes the built binary as a child process in an empty folder.
func runBin(t *testing.T, args ...string) result {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = t.TempDir()
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run %q: %v", args, err)
		}
		code = ee.ExitCode()
	}
	return result{out.String(), errb.String(), code}
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

func expect(t *testing.T, args []string, wantOut string, wantCode int) {
	t.Helper()
	r := runBin(t, args...)
	if r.stdout != wantOut || r.code != wantCode {
		t.Errorf("cron-next %q:\n got  code %d stdout %q stderr %q\n want code %d stdout %q", args, r.code, r.stdout, r.stderr, wantCode, wantOut)
	}
}

func expectUsage(t *testing.T, args ...string) {
	t.Helper()
	r := runBin(t, args...)
	if r.code != 2 || r.stdout != "" || !strings.HasPrefix(r.stderr, "cron-next: ") {
		t.Errorf("cron-next %q: want exit 2, empty stdout, cron-next: message; got code %d stdout %q stderr %q", args, r.code, r.stdout, r.stderr)
	}
}

func TestFieldsPlain(t *testing.T) {
	expect(t, []string{"fields", "*/15 9-17 * * 1-5"}, `minute: 0 15 30 45
hour: 9 10 11 12 13 14 15 16 17
day_of_month: 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30 31
month: 1 2 3 4 5 6 7 8 9 10 11 12
day_of_week: 1 2 3 4 5
day_rule: and
`, 0)
}

func fieldLines(t *testing.T, expr string) map[string]string {
	t.Helper()
	r := runBin(t, "fields", expr)
	if r.code != 0 {
		t.Fatalf("fields %q: code %d stderr %q", expr, r.code, r.stderr)
	}
	m := map[string]string{}
	ls := lines(r.stdout)
	if len(ls) != 6 {
		t.Fatalf("fields %q: want 6 lines, got %q", expr, r.stdout)
	}
	for _, l := range ls {
		k, v, _ := strings.Cut(l, ": ")
		m[k] = v
	}
	return m
}

func TestFieldsExpansion(t *testing.T) {
	g := func(expr, key, want string) {
		t.Helper()
		if got := fieldLines(t, expr)[key]; got != want {
			t.Errorf("fields %q %s: got %q want %q", expr, key, got, want)
		}
	}
	g("*/59 * * * *", "minute", "0 59")
	g("1-5/9 * * * *", "minute", "1")
	g("1,*/20 * * * *", "minute", "0 1 20 40")
	g("5,5,3-6 * * * *", "minute", "3 4 5 6")
	g("0-59/30 * * * *", "minute", "0 30")
	g("05 * * * *", "minute", "5")
	g("* */6 * * *", "hour", "0 6 12 18")
	g("* * */15 * *", "day_of_month", "1 16 31")
	g("* * 31,1 * *", "day_of_month", "1 31")
	g("* * * jan,Feb,DEC *", "month", "1 2 12")
	g("* * * Mar-MAY *", "month", "3 4 5")
	g("* * * */5 *", "month", "1 6 11")
	g("* * * * 7", "day_of_week", "0")
	g("* * * * 0-7", "day_of_week", "0 1 2 3 4 5 6")
	g("* * * * 5-7", "day_of_week", "0 5 6")
	g("* * * * *", "day_of_week", "0 1 2 3 4 5 6")
	g("* * * * */2", "day_of_week", "0 2 4 6")
	g("* * * * */7", "day_of_week", "0")
	g("* * * * 1-7/2", "day_of_week", "0 1 3 5")
	g("* * * * SUN,mon,Tue,WED,thu,Fri,sat", "day_of_week", "0 1 2 3 4 5 6")
	g("* * * * MON-5", "day_of_week", "1 2 3 4 5")
	g("* * * * sun-sat", "day_of_week", "0 1 2 3 4 5 6")
	g("* * * * 7,SUN", "day_of_week", "0")
	g(" \t5\t\t6  *   * *  ", "minute", "5")
	g(" \t5\t\t6  *   * *  ", "hour", "6")
	if got := fieldLines(t, "* * * * *")["day_of_month"]; strings.Count(got, " ") != 30 {
		t.Errorf("day_of_month of *: %q", got)
	}
}

func TestDayRuleField(t *testing.T) {
	rules := map[string]string{
		"0 0 * * *":       "and",
		"0 0 */2 * 1":     "and",
		"0 0 1 * *":       "and",
		"0 0 * * 1":       "and",
		"0 0 15 * 1":      "or",
		"0 0 1-31 * 0-7":  "or",
		"0 0 1-31 * MON":  "or",
		"0 0 *,5 * 1":     "and",
		"0 0 5,* * 1":     "or",
		"0 0 1 * *,5":     "and",
		"0 0 1-31 * 1-5/": "",
	}
	for expr, want := range rules {
		if want == "" {
			continue
		}
		if got := fieldLines(t, expr)["day_rule"]; got != want {
			t.Errorf("day_rule of %q: got %q want %q", expr, got, want)
		}
	}
}

func TestMacros(t *testing.T) {
	macros := map[string]string{
		"@yearly": "0 0 1 1 *", "@annually": "0 0 1 1 *", "@monthly": "0 0 1 * *", "@weekly": "0 0 * * 0",
		"@daily": "0 0 * * *", "@midnight": "0 0 * * *", "@hourly": "0 * * * *",
		"@DAILY": "0 0 * * *", "@Weekly": "0 0 * * 0", "  @hourly\t": "0 * * * *",
	}
	for m, full := range macros {
		a, b := runBin(t, "fields", m), runBin(t, "fields", full)
		if a.code != 0 || a.stdout != b.stdout {
			t.Errorf("macro %q: got %q (code %d), want %q", m, a.stdout, a.code, b.stdout)
		}
	}
	for _, bad := range []string{"@reboot", "@daily extra", "@dail", "@", "@daily ", "@dailyy", "@ daily", "daily", "@DAİLY", "@daily@", "*"} {
		if bad == "@daily " {
			continue // trailing blanks are ignored: valid
		}
		expectUsage(t, "fields", bad)
		expectUsage(t, "next", bad, "--from", "2026-01-01T00:00:00Z")
		expectUsage(t, "check", bad)
	}
	expect(t, []string{"fields", "@daily "}, runBin(t, "fields", "@daily").stdout, 0)
}

func TestInvalidExpressions(t *testing.T) {
	bad := []string{
		"", " ", "\t", "* * * *", "* * * * * *", "* * * * * * *", "*", "5",
		"60 * * * *", "* 24 * * *", "0 0 0 * *", "0 0 32 * *", "0 0 * 0 *", "0 0 * 13 *", "0 0 * * 8",
		"-1 * * * *", "+1 * * * *", "1.5 * * * *", "005 * * * *", "1e1 * * * *", "0x1 * * * *",
		"5-3 * * * *", "22-2 * * * *", "* * * * FRI-SUN", "* * * * 7-0", "* * * DEC-JAN *",
		"*/0 * * * *", "*/60 * * * *", "* */24 * * *", "* * */32 * *", "* * * */13 *", "* * * * */8",
		"*/005 * * * *", "*/-1 * * * *", "*/ * * * *", "5/10 * * * *", "5/ * * * *", "*/5/5 * * * *", "1-5/0 * * * *",
		"1-5/ * * * *", "1,,2 * * * *", ",1 * * * *", "1, * * * *", ", * * * *", "1-2-3 * * * *", "1- * * * *", "-5 * * * *",
		"** * * * *", "*5 * * * *", "*-5 * * * *", "1-* * * * *", "*/*  * * * *",
		"? * * * *", "* * ? * *", "* * * * ?", "* * L * *", "* * 5W * *", "* * LW * *", "* * * * 5L", "* * * * 5#2", "* * * * L",
		"0 0 1 * * 2026", "0 0 0 * * *", "H * * * *", "* H * * *",
		"* * * * MONDAY", "* * * JANUARY *", "* * * * mo", "* * * * monn", "* * * foo *", "* * * * foo",
		"MON * * * *", "* JAN * * *", "* * JAN * *", "* * * MON *", "* * * * JAN",
		"* * * * MON/2", "* * * JAN/2 *", "* * * * MON-FRI/MON", "* * * * 1-MON/2x",
		"* * * * １", "* * * * ５", "1\u00a0* * * *", "*\n* * * *", "*\v* * * *", "* * * * *\n",
		"0 0 1 1 * @daily", "@daily @daily",
	}
	for _, e := range bad {
		expectUsage(t, "fields", e)
		expectUsage(t, "check", e)
		expectUsage(t, "next", e, "--from", "2026-01-01T00:00:00Z")
		expectUsage(t, "fields", e, "--json")
		if strings.TrimSpace(e) != "" && !strings.ContainsAny(e, "\n") {
			// the message holds the argument's text as given
			if r := runBin(t, "fields", e); !strings.Contains(r.stderr, e) {
				t.Errorf("stderr for %q does not hold the expression: %q", e, r.stderr)
			}
		}
	}
}

func TestValidExpressionsExtra(t *testing.T) {
	for _, e := range []string{"0 0 1 1 *", "59 23 31 12 7", "00 00 01 01 00", "*/1 */1 */1 */1 */1", "0-59 0-23 1-31 1-12 0-7", "5,10-20/3,*/30 * * * *", "* * * JAN-DEC SUN-SAT", "  0   0 \t 1 1 * "} {
		if r := runBin(t, "fields", e); r.code != 0 {
			t.Errorf("fields %q: code %d stderr %q", e, r.code, r.stderr)
		}
	}
}

func TestUsageErrors(t *testing.T) {
	from := []string{"--from", "2026-01-01T00:00:00Z"}
	cases := [][]string{
		{},
		{"next"},
		{"check"},
		{"fields"},
		{"bogus", "* * * * *"},
		{"NEXT", "* * * * *"},
		{"", "* * * * *"},
		{"next", "* * * * *", "extra"},
		{"next", "*", "*", "*", "*", "*"},
		{"check", "* * * * *", "x"},
		{"fields", "0", "0", "*", "*", "*"},
		{"--json"},
		{"-"},
		{"--"},
		{"next", "--", "* * * * *"},
		{"next", "-", "* * * * *"},
		{"next", "* * * * *", "--count=3"},
		{"next", "* * * * *", "--tz=UTC"},
		{"next", "* * * * *", "--from=2026-01-01T00:00:00Z"},
		{"next", "* * * * *", "-x"},
		{"next", "* * * * *", "-c", "3"},
		{"next", "* * * * *", "--bogus"},
		{"next", "* * * * *", "--JSON"},
		{"next", "* * * * *", "--count"},
		{"next", "* * * * *", "--from"},
		{"next", "* * * * *", "--tz"},
		{"next", "* * * * *", "--count", "3", "--count", "4"},
		{"next", "* * * * *", "--count", "3", "--count", "3"},
		{"next", "* * * * *", "--from", "2026-01-01T00:00:00Z", "--from", "2026-01-01T00:00:00Z"},
		{"next", "* * * * *", "--tz", "UTC", "--tz", "UTC"},
		{"next", "* * * * *", "--count", "--json"},
		{"next", "* * * * *", "--tz", "--json"},
		{"next", "--tz", "* * * * *"},
		{"check", "* * * * *", "--tz", "UTC"},
		{"check", "* * * * *", "--count", "3"},
		{"check", "* * * * *", "--from", "2026-01-01T00:00:00Z"},
		{"fields", "* * * * *", "--tz", "UTC"},
		{"fields", "* * * * *", "--count", "3"},
		{"fields", "* * * * *", "--from", "2026-01-01T00:00:00Z"},
		{"--help", "next"},
		{"next", "--help"},
		{"next", "* * * * *", "--help"},
		{"-h", "next"},
		{"check", "* * * * *", "-h"},
		{"--help", "--json"},
		{"--version", "--json"},
		{"next", "* * * * *", "--version"},
		{"--help", "--help"},
		{"-h", "-h"},
		{"--version", "--version"},
	}
	for _, c := range cases {
		expectUsage(t, c...)
	}
	// every check above ran without --from where needed; also with a good one
	expectUsage(t, append([]string{"bogus", "* * * * *"}, from...)...)
}

func TestOptionPositions(t *testing.T) {
	want := runBin(t, "next", "0 9 * * *", "--from", "2026-01-01T00:00:00Z", "--count", "2", "--tz", "Asia/Tokyo")
	// 2026-01-01T09:00+09:00 is the --from instant itself: not an answer
	if want.code != 0 || want.stdout != "2026-01-02T09:00:00+09:00\n2026-01-03T09:00:00+09:00\n" {
		t.Fatalf("unexpected: %+v", want)
	}
	variants := [][]string{
		{"--from", "2026-01-01T00:00:00Z", "--count", "2", "--tz", "Asia/Tokyo", "next", "0 9 * * *"},
		{"--count", "2", "next", "--tz", "Asia/Tokyo", "0 9 * * *", "--from", "2026-01-01T00:00:00Z"},
		{"next", "--tz", "Asia/Tokyo", "--from", "2026-01-01T00:00:00Z", "0 9 * * *", "--count", "2"},
		{"--tz", "Asia/Tokyo", "--count", "2", "--from", "2026-01-01T00:00:00Z", "next", "0 9 * * *"},
	}
	for _, v := range variants {
		if got := runBin(t, v...); got != want {
			t.Errorf("%q: got %+v want %+v", v, got, want)
		}
	}
	// a value is never read as an option or a command
	r := runBin(t, "next", "0 9 * * *", "--tz", "next", "--from", "2026-01-01T00:00:00Z")
	if r.code != 2 {
		t.Errorf("--tz next: %+v", r)
	}
	a := runBin(t, "--json", "check", "0 0 29 2 *", "--json")
	b := runBin(t, "check", "--json", "--json", "0 0 29 2 *")
	if a != b || a.code != 0 || a.stdout != `{"expression":"0 0 29 2 *","valid":true,"fires":true}`+"\n" {
		t.Errorf("--json twice: %+v / %+v", a, b)
	}
}

func TestHelpAndVersion(t *testing.T) {
	for _, a := range []string{"--help", "-h"} {
		r := runBin(t, a)
		first := strings.SplitN(r.stdout, "\n", 2)[0]
		if r.code != 0 || !strings.HasPrefix(first, "usage: cron-next") || r.stderr != "" {
			t.Errorf("%s: %+v", a, r)
		}
		for _, w := range []string{"next", "check", "fields"} {
			if !strings.Contains(r.stdout, w) {
				t.Errorf("%s: usage does not name %s", a, w)
			}
		}
	}
	expect(t, []string{"--version"}, "cron-next 0.1.0\n", 0)
}

func TestNextBasics(t *testing.T) {
	expect(t, []string{"next", "*/15 9-17 * * 1-5", "--from", "2026-01-01T00:00:00Z", "--count", "6"},
		"2026-01-01T09:00:00+00:00\n2026-01-01T09:15:00+00:00\n2026-01-01T09:30:00+00:00\n2026-01-01T09:45:00+00:00\n2026-01-01T10:00:00+00:00\n2026-01-01T10:15:00+00:00\n", 0)
	// default count is 5
	r := runBin(t, "next", "* * * * *", "--from", "2026-01-01T00:00:00Z")
	if len(lines(r.stdout)) != 5 || r.code != 0 {
		t.Errorf("default count: %+v", r)
	}
	// 1000 is the most; 1 the least
	if r := runBin(t, "next", "* * * * *", "--from", "2026-01-01T00:00:00Z", "--count", "1000"); len(lines(r.stdout)) != 1000 {
		t.Errorf("count 1000: %d lines", len(lines(r.stdout)))
	}
	expect(t, []string{"next", "@hourly", "--from", "2026-01-01T00:00:00Z", "--count", "1"}, "2026-01-01T01:00:00+00:00\n", 0)
	expect(t, []string{"next", "@yearly", "--from", "2026-06-01T00:00:00Z", "--count", "2"}, "2027-01-01T00:00:00+00:00\n2028-01-01T00:00:00+00:00\n", 0)
	expect(t, []string{"next", "@weekly", "--from", "2026-01-01T00:00:00Z", "--count", "2"}, "2026-01-04T00:00:00+00:00\n2026-01-11T00:00:00+00:00\n", 0) // 2026-01-04 is a Sunday
	expect(t, []string{"next", "0 0 29 2 *", "--from", "2026-01-01T00:00:00Z", "--count", "2"}, "2028-02-29T00:00:00+00:00\n2032-02-29T00:00:00+00:00\n", 0)
}

func TestStrictlyAfterFrom(t *testing.T) {
	for _, from := range []string{
		"2026-01-01T00:00:00Z", "2026-01-01T00:00:30Z", "2026-01-01T00:00:59.999999999Z",
		"2026-01-01T00:00:00.5Z", "2026-01-01T05:30:00+05:30", "2025-12-31T19:00:00-05:00", "2026-01-01T00:00:00.000000001+00:00",
	} {
		expect(t, []string{"next", "* * * * *", "--from", from, "--count", "2"}, "2026-01-01T00:01:00+00:00\n2026-01-01T00:02:00+00:00\n", 0)
	}
	expect(t, []string{"next", "* * * * *", "--from", "2025-12-31T23:59:59.999Z", "--count", "1"}, "2026-01-01T00:00:00+00:00\n", 0)
	expect(t, []string{"next", "* * * * *", "--from", "2025-12-31T23:59:59Z", "--count", "1"}, "2026-01-01T00:00:00+00:00\n", 0)
	// the same instant read in another zone
	expect(t, []string{"next", "30 5 * * *", "--tz", "Asia/Kolkata", "--from", "2026-01-01T05:30:00+05:30", "--count", "1"}, "2026-01-02T05:30:00+05:30\n", 0)
	expect(t, []string{"next", "30 5 * * *", "--tz", "Asia/Kolkata", "--from", "2026-01-01T00:00:00Z", "--count", "1"}, "2026-01-02T05:30:00+05:30\n", 0)
	expect(t, []string{"next", "30 5 * * *", "--tz", "Asia/Kolkata", "--from", "2026-01-01T00:00:00Z", "--count", "1", "--json"},
		`{"expression":"30 5 * * *","timezone":"Asia/Kolkata","from":"2026-01-01T05:30:00+05:30","times":["2026-01-02T05:30:00+05:30"]}`+"\n", 0)
}

func TestDayRuleNext(t *testing.T) {
	// 2026-01-01 is a Thursday: the 15th is a Thursday, Mondays are 5, 12, 19, 26.
	expect(t, []string{"next", "0 0 15 * 1", "--from", "2026-01-01T00:00:00Z", "--count", "6"},
		"2026-01-05T00:00:00+00:00\n2026-01-12T00:00:00+00:00\n2026-01-15T00:00:00+00:00\n2026-01-19T00:00:00+00:00\n2026-01-26T00:00:00+00:00\n2026-02-02T00:00:00+00:00\n", 0)
	expect(t, []string{"next", "0 0 1-31 * 1", "--from", "2026-01-01T00:00:00Z", "--count", "3"},
		"2026-01-02T00:00:00+00:00\n2026-01-03T00:00:00+00:00\n2026-01-04T00:00:00+00:00\n", 0)
	// */2 is odd days: Mondays on odd days of the month, and
	expect(t, []string{"next", "0 0 */2 * 1", "--from", "2026-01-01T00:00:00Z", "--count", "4"},
		"2026-01-05T00:00:00+00:00\n2026-01-19T00:00:00+00:00\n2026-02-09T00:00:00+00:00\n2026-02-23T00:00:00+00:00\n", 0)
	expect(t, []string{"next", "0 0 * * 1", "--from", "2026-01-01T00:00:00Z", "--count", "2"},
		"2026-01-05T00:00:00+00:00\n2026-01-12T00:00:00+00:00\n", 0)
	expect(t, []string{"next", "0 0 13 * *", "--from", "2026-01-01T00:00:00Z", "--count", "2"},
		"2026-01-13T00:00:00+00:00\n2026-02-13T00:00:00+00:00\n", 0)
	// both restricted: the 13th or a Friday
	expect(t, []string{"next", "0 0 13 * 5", "--from", "2026-01-01T00:00:00Z", "--count", "3"},
		"2026-01-02T00:00:00+00:00\n2026-01-09T00:00:00+00:00\n2026-01-13T00:00:00+00:00\n", 0)
	// *,5 starts with a star: the weekday field is starred, so the rule is "and": the 13th, any weekday
	expect(t, []string{"next", "0 0 13 * *,5", "--from", "2026-01-01T00:00:00Z", "--count", "1"}, "2026-01-13T00:00:00+00:00\n", 0)
	// 5,* does not: both are restricted, "or"
	expect(t, []string{"next", "0 0 13 * 5,*", "--from", "2026-01-01T00:00:00Z", "--count", "2"}, "2026-01-02T00:00:00+00:00\n2026-01-03T00:00:00+00:00\n", 0)
	// month restricts first
	expect(t, []string{"next", "0 0 15 mar mon", "--from", "2026-01-01T00:00:00Z", "--count", "3"},
		"2026-03-02T00:00:00+00:00\n2026-03-09T00:00:00+00:00\n2026-03-15T00:00:00+00:00\n", 0)
	expect(t, []string{"next", "0 0 1 1 7", "--from", "2026-01-01T00:00:00Z", "--count", "2"},
		"2026-01-04T00:00:00+00:00\n2026-01-11T00:00:00+00:00\n", 0)
}

func TestNeverFires(t *testing.T) {
	for _, e := range []string{"0 0 31 2 *", "0 0 30 2 *", "0 0 31 4,6,9,11 *", "0 0 30 2 */2", "0 0 31 apr *"} {
		expect(t, []string{"next", e, "--from", "2026-01-01T00:00:00Z"}, "", 1)
		expect(t, []string{"check", e}, "valid: never fires\n", 1)
		expect(t, []string{"check", e, "--json"}, fmt.Sprintf(`{"expression":%q,"valid":true,"fires":false}`+"\n", e), 1)
		expect(t, []string{"next", e, "--from", "2026-01-01T00:00:00Z", "--json", "--tz", "Europe/Berlin"},
			fmt.Sprintf(`{"expression":%q,"timezone":"Europe/Berlin","from":"2026-01-01T01:00:00+01:00","times":[]}`+"\n", e), 1)
		if r := runBin(t, "fields", e); r.code != 0 || r.stdout == "" {
			t.Errorf("fields %q: %+v", e, r)
		}
	}
}

func TestCheck(t *testing.T) {
	for _, e := range []string{"0 0 29 2 *", "0 0 31 2 1", "0 0 30 2 0-6", "0 0 30 2 0-7", "* * * * *", "0 0 31 * *", "0 0 31 2 mon", "@yearly", "0 0 29 feb 7"} {
		expect(t, []string{"check", e}, "valid: can fire\n", 0)
		expect(t, []string{"check", e, "--json"}, fmt.Sprintf(`{"expression":%q,"valid":true,"fires":true}`+"\n", e), 0)
	}
	// the either-or rule: both restricted, so a Monday in February is enough
	expect(t, []string{"check", "0 0 31 2 *,1"}, "valid: never fires\n", 1)
}

func TestFieldsJSON(t *testing.T) {
	r := runBin(t, "fields", "*/20 9 1,15 JAN-MAR 7", "--json")
	if r.code != 0 || !strings.HasSuffix(r.stdout, "}\n") || strings.Count(r.stdout, "\n") != 1 {
		t.Fatalf("%+v", r)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &m); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"expression": "*/20 9 1,15 JAN-MAR 7", "minute": []any{0.0, 20.0, 40.0}, "hour": []any{9.0},
		"day_of_month": []any{1.0, 15.0}, "month": []any{1.0, 2.0, 3.0}, "day_of_week": []any{0.0}, "day_rule": "or",
	}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("got %v want %v", m, want)
	}
}

func TestNextJSONKeys(t *testing.T) {
	r := runBin(t, "next", "0 9 * * 1-5", "--json", "--from", "2026-03-05T08:30:15.123456789Z", "--count", "2", "--tz", "America/New_York")
	var m map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &m); err != nil || r.code != 0 || strings.Count(r.stdout, "\n") != 1 {
		t.Fatalf("%v %+v", err, r)
	}
	want := map[string]any{
		"expression": "0 9 * * 1-5", "timezone": "America/New_York", "from": "2026-03-05T03:30:15-05:00",
		"times": []any{"2026-03-05T09:00:00-05:00", "2026-03-06T09:00:00-05:00"},
	}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("got %v want %v", m, want)
	}
	// text is encoded as JSON requires: tabs in the expression
	r = runBin(t, "next", "0\t9 * * *", "--json", "--from", "2026-01-01T00:00:00Z", "--count", "1")
	if err := json.Unmarshal([]byte(r.stdout), &m); err != nil || m["expression"] != "0\t9 * * *" {
		t.Errorf("tab expression: %v %q", err, r.stdout)
	}
}

func TestZonesOffsets(t *testing.T) {
	cases := []struct{ tz, expr, from, want string }{
		{"UTC", "0 9 1 1 *", "2025-06-01T00:00:00Z", "2026-01-01T09:00:00+00:00"},
		{"Asia/Kolkata", "0 9 1 1 *", "2025-06-01T00:00:00Z", "2026-01-01T09:00:00+05:30"},
		{"Asia/Kathmandu", "0 9 1 1 *", "2025-06-01T00:00:00Z", "2026-01-01T09:00:00+05:45"},
		{"Pacific/Chatham", "0 9 1 1 *", "2025-06-01T00:00:00Z", "2026-01-01T09:00:00+13:45"},
		{"Pacific/Kiritimati", "0 9 1 1 *", "2025-06-01T00:00:00Z", "2026-01-01T09:00:00+14:00"},
		{"Pacific/Pago_Pago", "0 9 1 1 *", "2025-06-01T00:00:00Z", "2026-01-01T09:00:00-11:00"},
		{"America/St_Johns", "0 9 1 1 *", "2025-06-01T00:00:00Z", "2026-01-01T09:00:00-03:30"},
		{"Australia/Lord_Howe", "0 9 1 1 *", "2025-06-01T00:00:00Z", "2026-01-01T09:00:00+11:00"},
		{"Australia/Lord_Howe", "0 9 1 7 *", "2026-06-01T00:00:00Z", "2026-07-01T09:00:00+10:30"},
		{"Etc/GMT+12", "0 9 1 1 *", "2025-06-01T00:00:00Z", "2026-01-01T09:00:00-12:00"},
	}
	for _, c := range cases {
		expect(t, []string{"next", c.expr, "--tz", c.tz, "--from", c.from, "--count", "1"}, c.want+"\n", 0)
	}
	for _, line := range lines(runBin(t, "next", "* * * * *", "--from", "2026-01-01T00:00:00Z", "--tz", "UTC", "--count", "3").stdout) {
		if strings.Contains(line, "Z") {
			t.Errorf("Z in %q", line)
		}
	}
}

func TestDaylightSaving(t *testing.T) {
	// New York, 2026-03-08: 02:00 jumps to 03:00. 02:30 does not exist.
	expect(t, []string{"next", "30 2 * * *", "--tz", "America/New_York", "--from", "2026-03-07T00:00:00Z", "--count", "3"},
		"2026-03-07T02:30:00-05:00\n2026-03-09T02:30:00-04:00\n2026-03-10T02:30:00-04:00\n", 0)
	expect(t, []string{"next", "0 * * * *", "--tz", "America/New_York", "--from", "2026-03-08T05:30:00Z", "--count", "3"},
		"2026-03-08T01:00:00-05:00\n2026-03-08T03:00:00-04:00\n2026-03-08T04:00:00-04:00\n", 0)
	expect(t, []string{"next", "*/30 2 * * *", "--tz", "America/New_York", "--from", "2026-03-08T05:00:00Z", "--count", "2"},
		"2026-03-09T02:00:00-04:00\n2026-03-09T02:30:00-04:00\n", 0)
	// New York, 2026-11-01: 02:00 back to 01:00: 01:30 happens twice.
	expect(t, []string{"next", "30 1 * * *", "--tz", "America/New_York", "--from", "2026-10-31T12:00:00Z", "--count", "4"},
		"2026-11-01T01:30:00-04:00\n2026-11-01T01:30:00-05:00\n2026-11-02T01:30:00-05:00\n2026-11-03T01:30:00-05:00\n", 0)
	// 2027: 14 March and 7 November
	expect(t, []string{"next", "30 2 * * *", "--tz", "America/New_York", "--from", "2027-03-13T12:00:00Z", "--count", "2"},
		"2027-03-15T02:30:00-04:00\n2027-03-16T02:30:00-04:00\n", 0)
	expect(t, []string{"next", "30 1 * * *", "--tz", "America/New_York", "--from", "2027-11-06T12:00:00Z", "--count", "3"},
		"2027-11-07T01:30:00-04:00\n2027-11-07T01:30:00-05:00\n2027-11-08T01:30:00-05:00\n", 0)

	// Berlin, 2026-03-29: 02:00 to 03:00; 2026-10-25: 03:00 back to 02:00.
	expect(t, []string{"next", "30 2 * * *", "--tz", "Europe/Berlin", "--from", "2026-03-28T12:00:00Z", "--count", "2"},
		"2026-03-30T02:30:00+02:00\n2026-03-31T02:30:00+02:00\n", 0)
	expect(t, []string{"next", "30 2 * * *", "--tz", "Europe/Berlin", "--from", "2026-10-24T12:00:00Z", "--count", "3"},
		"2026-10-25T02:30:00+02:00\n2026-10-25T02:30:00+01:00\n2026-10-26T02:30:00+01:00\n", 0)
	expect(t, []string{"next", "0 3 * * *", "--tz", "Europe/Berlin", "--from", "2026-10-24T12:00:00Z", "--count", "2"},
		"2026-10-25T03:00:00+01:00\n2026-10-26T03:00:00+01:00\n", 0)
	expect(t, []string{"next", "0 2-3 25 10 *", "--tz", "Europe/Berlin", "--from", "2026-10-24T12:00:00Z", "--count", "3"},
		"2026-10-25T02:00:00+02:00\n2026-10-25T02:00:00+01:00\n2026-10-25T03:00:00+01:00\n", 0)
	expect(t, []string{"next", "30 2 * * *", "--tz", "Europe/Berlin", "--from", "2027-03-27T12:00:00Z", "--count", "2"},
		"2027-03-29T02:30:00+02:00\n2027-03-30T02:30:00+02:00\n", 0)
	expect(t, []string{"next", "30 2 * * *", "--tz", "Europe/Berlin", "--from", "2027-10-30T12:00:00Z", "--count", "3"},
		"2027-10-31T02:30:00+02:00\n2027-10-31T02:30:00+01:00\n2027-11-01T02:30:00+01:00\n", 0)

	// Lord Howe moves by 30 minutes: 2026-10-04 02:00 to 02:30; 2026-04-05 02:00 back to 01:30.
	expect(t, []string{"next", "15 2 * * *", "--tz", "Australia/Lord_Howe", "--from", "2026-10-01T00:00:00Z", "--count", "3"},
		"2026-10-02T02:15:00+10:30\n2026-10-03T02:15:00+10:30\n2026-10-05T02:15:00+11:00\n", 0)
	expect(t, []string{"next", "0 2 * * *", "--tz", "Australia/Lord_Howe", "--from", "2026-10-03T00:00:00Z", "--count", "2"},
		"2026-10-05T02:00:00+11:00\n2026-10-06T02:00:00+11:00\n", 0)
	expect(t, []string{"next", "30 2 * * *", "--tz", "Australia/Lord_Howe", "--from", "2026-10-03T00:00:00Z", "--count", "2"},
		"2026-10-04T02:30:00+11:00\n2026-10-05T02:30:00+11:00\n", 0)
	expect(t, []string{"next", "45 1 * * *", "--tz", "Australia/Lord_Howe", "--from", "2026-04-04T00:00:00Z", "--count", "3"},
		"2026-04-05T01:45:00+11:00\n2026-04-05T01:45:00+10:30\n2026-04-06T01:45:00+10:30\n", 0)
	expect(t, []string{"next", "0 2 * * *", "--tz", "Australia/Lord_Howe", "--from", "2026-04-04T00:00:00Z", "--count", "2"},
		"2026-04-05T02:00:00+10:30\n2026-04-06T02:00:00+10:30\n", 0)
}

func TestSearchWindow(t *testing.T) {
	// Century years: 2100, 2200 and 2300 are not leap years, 2400 is.
	r := runBin(t, "next", "0 0 29 2 *", "--from", "2026-01-01T00:00:00Z", "--count", "1000")
	ls := lines(r.stdout)
	if r.code != 0 || len(ls) != 97 || ls[96] != "2424-02-29T00:00:00+00:00" {
		t.Errorf("29 Feb: code %d, %d lines, last %q", r.code, len(ls), ls[len(ls)-1])
	}
	for _, l := range ls {
		for _, no := range []string{"2100-", "2200-", "2300-"} {
			if strings.HasPrefix(l, no) {
				t.Errorf("fires in %s", l)
			}
		}
	}
	expect(t, []string{"next", "0 0 29 2 *", "--from", "2096-01-01T00:00:00Z", "--count", "3"},
		"2096-02-29T00:00:00+00:00\n2104-02-29T00:00:00+00:00\n2108-02-29T00:00:00+00:00\n", 0)
	// the last instant of the window is included, the first is not
	r = runBin(t, "next", "0 0 1 1 *", "--from", "2026-01-01T00:00:00Z", "--count", "1000")
	ls = lines(r.stdout)
	if len(ls) != 400 || ls[0] != "2027-01-01T00:00:00+00:00" || ls[399] != "2426-01-01T00:00:00+00:00" {
		t.Errorf("window end included: %d lines %q", len(ls), ls)
	}
	r = runBin(t, "next", "0 0 1 1 *", "--from", "2025-12-31T23:59:59Z", "--count", "1000")
	ls = lines(r.stdout)
	if len(ls) != 400 || ls[0] != "2026-01-01T00:00:00+00:00" || ls[399] != "2425-01-01T00:00:00+00:00" {
		t.Errorf("window: %d lines", len(ls))
	}
	// the bound is 146097 days after the instant: an instant one second later is out
	r = runBin(t, "next", "0 0 1 1 *", "--from", "2025-12-31T23:59:59Z", "--count", "1000", "--tz", "Pacific/Kiritimati")
	if ls = lines(r.stdout); len(ls) != 400 {
		t.Errorf("window in zone far from UTC: %d lines", len(ls))
	}
	// a schedule with fewer fire times than --count is not an error
	r = runBin(t, "next", "0 0 29 2 1", "--from", "2026-01-01T00:00:00Z", "--count", "1000")
	if r.code != 0 || len(lines(r.stdout)) != 1000 {
		t.Errorf("29 Feb or Monday: %d", len(lines(r.stdout)))
	}
	// from the last allowed instant: 2200 and 2300 are skipped, 2400 is not, and 2500 is out of the window anyway
	r = runBin(t, "next", "0 0 29 2 *", "--from", "2100-12-31T23:59:59Z", "--count", "1000")
	ls = lines(r.stdout)
	if r.code != 0 || len(ls) != 97 || ls[0] != "2104-02-29T00:00:00+00:00" || ls[96] != "2496-02-29T00:00:00+00:00" {
		t.Errorf("late: %d lines, %q", len(ls), ls)
	}
	// 2100-12-31T23:59:59Z is the last --from
	expect(t, []string{"next", "0 0 1 1 *", "--from", "2100-12-31T23:59:59Z", "--count", "1"}, "2101-01-01T00:00:00+00:00\n", 0)
	expect(t, []string{"next", "0 0 1 1 *", "--from", "1970-01-01T00:00:00Z", "--count", "1"}, "1971-01-01T00:00:00+00:00\n", 0)
}

func TestCountValidation(t *testing.T) {
	from := []string{"--from", "2026-01-01T00:00:00Z"}
	for _, bad := range []string{"0", "1001", "-1", "+1", "01", "00", "001", "1.5", "1e2", "", " 1", "1 ", "10000", "99999999999999999999", "x", "０１", "０", "٣", "0x10", "1_0"} {
		expectUsage(t, append([]string{"next", "* * * * *", "--count", bad}, from...)...)
	}
	for _, good := range []string{"1", "9", "10", "999", "1000", "100"} {
		r := runBin(t, append([]string{"next", "* * * * *", "--count", good}, from...)...)
		if r.code != 0 || fmt.Sprint(len(lines(r.stdout))) != good {
			t.Errorf("--count %s: %d lines, code %d", good, len(lines(r.stdout)), r.code)
		}
	}
}

func TestFromValidation(t *testing.T) {
	good := []string{
		"2026-01-01T00:00:00Z", "2026-01-01T00:00:00.1Z", "2026-01-01T00:00:00.123456789Z", "2026-01-01T00:00:00+00:00",
		"2026-01-01T00:00:00-00:00", "2026-01-01T23:59:59+23:59", "2026-01-01T00:00:00-23:59", "2028-02-29T12:00:00Z",
		"2000-02-29T12:00:00Z", "2100-12-31T23:59:59Z", "1970-01-01T00:00:00Z", "1970-01-01T05:30:00+05:30", "2100-12-31T23:59:59-00:00",
		"2101-01-01T00:00:00+05:30", "2100-12-31T23:59:59.000000000Z",
	}
	for _, f := range good {
		r := runBin(t, "next", "* * * * *", "--from", f, "--count", "1")
		if r.code != 0 || len(lines(r.stdout)) != 1 {
			t.Errorf("--from %q: %+v", f, r)
		}
	}
	bad := []string{
		"", "2026-01-01", "2026-01-01T00:00Z", "2026-01-01T00:00:00", "2026-01-01 00:00:00Z", "2026-01-01t00:00:00Z", "2026-01-01T00:00:00z",
		"2026-01-01T00:00:60Z", "2026-01-01T24:00:00Z", "2026-01-01T00:60:00Z", "2026-13-01T00:00:00Z", "2026-00-01T00:00:00Z",
		"2026-01-00T00:00:00Z", "2026-02-29T00:00:00Z", "2100-02-29T00:00:00Z", "2026-04-31T00:00:00Z", "2026-01-32T00:00:00Z",
		"2026-01-01T00:00:00.Z", "2026-01-01T00:00:00.1234567890Z", "2026-01-01T00:00:00,5Z", "2026-01-01T00:00:00+0100",
		"2026-01-01T00:00:00+01", "2026-01-01T00:00:00+24:00", "2026-01-01T00:00:00+01:60", "2026-01-01T00:00:00 +01:00",
		"2026-01-01T00:00:00+1:00", "2026-01-01T00:00:00Z ", " 2026-01-01T00:00:00Z", "2026-1-1T0:0:0Z", "26-01-01T00:00:00Z",
		"1969-12-31T23:59:59Z", "1970-01-01T00:00:00+00:01", "1970-01-01T00:00:00+01:00", "2101-01-01T00:00:00Z", "2100-12-31T23:59:59.5Z",
		"2100-12-31T23:59:59-00:01", "2101-01-01T00:00:00+00:00", "0000-01-01T00:00:00Z", "9999-12-31T23:59:59Z", "2026-01-01T00:00:00ZZ",
		"2026-01-01T00:00:00+01:00Z", "2026-01-01T00:00:00Europe/Berlin", "1767225600", "now", "２０２６-01-01T00:00:00Z", "2026-01-01T00:00:00+０１:00",
		"2026-01-01T00:00:00.١Z",
	}
	for _, f := range bad {
		expectUsage(t, "next", "* * * * *", "--from", f)
		if f != "" {
			if r := runBin(t, "next", "* * * * *", "--from", f); !strings.Contains(r.stderr, f) {
				t.Errorf("stderr for --from %q does not hold it: %q", f, r.stderr)
			}
		}
	}
}

func TestTZValidation(t *testing.T) {
	from := []string{"--from", "2026-01-01T00:00:00Z"}
	for _, bad := range []string{"", "Local", "local", "Nowhere/Land", "europe/berlin", "EUROPE/BERLIN", "utc", "/Europe/Berlin", "/etc/localtime", "../Europe/Berlin", "Europe/../Europe/Berlin", "..", "Europe/", "Europe", "CET+1", "+01:00", "UTC+1", "Berlin", " UTC", "UTC ", "Europe\\Berlin", "\\Europe\\Berlin"} {
		expectUsage(t, append([]string{"next", "* * * * *", "--tz", bad}, from...)...)
		if bad != "" {
			if r := runBin(t, append([]string{"next", "* * * * *", "--tz", bad}, from...)...); !strings.Contains(r.stderr, bad) {
				t.Errorf("stderr for --tz %q does not hold it: %q", bad, r.stderr)
			}
		}
	}
	for _, good := range []string{"UTC", "Europe/Berlin", "America/New_York", "Asia/Kolkata", "Australia/Lord_Howe", "Etc/UTC", "America/Argentina/Buenos_Aires", "GMT", "Asia/Kathmandu"} {
		r := runBin(t, append([]string{"next", "* * * * *", "--tz", good, "--count", "1", "--json"}, from...)...)
		var m map[string]any
		if r.code != 0 || json.Unmarshal([]byte(r.stdout), &m) != nil || m["timezone"] != good {
			t.Errorf("--tz %s: %+v", good, r)
		}
	}
}

func TestNoStdinNoFiles(t *testing.T) {
	cmd := exec.Command(binary, "next", "* * * * *", "--from", "2026-01-01T00:00:00Z", "--count", "1")
	cmd.Dir = t.TempDir()
	cmd.Stdin = nil
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(cmd.Dir)
	if len(entries) != 0 {
		t.Errorf("files written: %v", entries)
	}
}

// naive finds fire times by looking at every minute, one by one, and reading
// the wall clock: the definition in the job, without any cleverness.
func naive(s *schedule, loc *time.Location, from int64, count int, until int64) []int64 {
	var out []int64
	for u := (from/60 + 1) * 60; u <= until && len(out) < count; u += 60 {
		tm := time.Unix(u, 0).In(loc)
		if _, off := tm.Zone(); off%60 != 0 {
			continue
		}
		if tm.Second() != 0 || !s.minute[tm.Minute()] || !s.hour[tm.Hour()] {
			continue
		}
		if s.dayMatches(int(tm.Month()), tm.Day(), int(tm.Weekday())) {
			out = append(out, u)
		}
	}
	return out
}

func TestAgainstNaiveScan(t *testing.T) {
	zones := []string{"UTC", "America/New_York", "Europe/Berlin", "Australia/Lord_Howe", "Asia/Kolkata", "Asia/Kathmandu", "Pacific/Chatham", "America/Sao_Paulo", "Africa/Casablanca"}
	exprs := []string{"*/7 * * * *", "30 2 * * *", "0 * * * *", "15,45 1-3 * * *", "0 0 * * 0", "59 23 28-31 * *", "0 2 1-31 3,10,11 6", "5 0,1,2,3 */3 * 2-4"}
	froms := []time.Time{
		time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 1, 7, 0, 0, 0, time.UTC),
		time.Date(2027, 2, 25, 0, 0, 0, 0, time.UTC),
		time.Date(2027, 10, 20, 0, 0, 0, 0, time.UTC),
	}
	for _, z := range zones {
		loc, err := time.LoadLocation(z)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range exprs {
			s, err := parseExpression(e)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range froms {
				from := f.Unix()
				want := naive(s, loc, from, 300, from+90*86400)
				got := s.fireTimes(loc, from, len(want))
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("%s %q from %s:\n got  %v\n want %v", z, e, f, got, want)
				}
				if !sort.SliceIsSorted(got, func(i, j int) bool { return got[i] < got[j] }) {
					t.Fatalf("unsorted")
				}
			}
		}
	}
}

func TestRunInProcess(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"next", "@daily", "--count", "2"}, time.Date(2026, 5, 5, 13, 0, 0, 0, time.UTC), &out, &errb)
	if code != 0 || out.String() != "2026-05-06T00:00:00+00:00\n2026-05-07T00:00:00+00:00\n" {
		t.Errorf("default from is the current time: %d %q", code, out.String())
	}
	out.Reset()
	errb.Reset()
	if code := run([]string{"next"}, time.Now(), &out, &errb); code != 2 || out.Len() != 0 || !strings.HasPrefix(errb.String(), "cron-next: ") {
		t.Errorf("%d %q %q", code, out.String(), errb.String())
	}
}
