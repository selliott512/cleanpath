package main

import (
	"regexp"
	"strings"
	"testing"
)

// TestCleanPath verifies basic normalization cases.
func TestCleanPath(t *testing.T) {
	cases := map[string]string{
		"./aa/bb":        "aa/bb",
		"/tmp/./aa//bb/": "/tmp/aa/bb",
		"/":              "/",
		"/../a":          "/a",
		"/../../":        "/",
		"../a":           "../a",
		"../..":          "../..",
		"a/../b":         "b",
		"":               ".",
		"a//b//c/..":     "a/b",
	}

	for input, want := range cases {
		got := cleanPath(input)
		if got != want {
			t.Fatalf("cleanPath(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestRunWithStdin verifies stdin input handling.
func TestRunWithStdin(t *testing.T) {
	in := "./aa/bb\n/tmp/./aa//bb/\n"
	var out, errOut strings.Builder

	code := run([]string{"-i"}, strings.NewReader(in), &out, &errOut)
	if code != 0 {
		t.Fatalf("run returned exit code %d, want 0 (stderr: %q)", code, errOut.String())
	}

	want := "aa/bb\n/tmp/aa/bb\n"
	if out.String() != want {
		t.Fatalf("run output = %q, want %q", out.String(), want)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", errOut.String())
	}
}

// TestRunUsage verifies usage output when no inputs are provided.
func TestRunUsage(t *testing.T) {
	var out, errOut strings.Builder
	code := run([]string{}, strings.NewReader(""), &out, &errOut)
	if code != 1 {
		t.Fatalf("run returned exit code %d, want 1", code)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", out.String())
	}
	if !strings.Contains(errOut.String(), "usage: cleanpath [options]") {
		t.Fatalf("stderr did not contain usage, got %q", errOut.String())
	}
}

// TestEnvExpand ensures env expansion applies to allowed variables.
func TestEnvExpand(t *testing.T) {
	t.Setenv("FOO", "bar")
	opts := options{
		envExpand: true,
		envAllowed: map[string]struct{}{
			"FOO": {},
		},
	}

	got := transformPath("$FOO/baz", opts)
	if got != "bar/baz" {
		t.Fatalf("transformPath env expand = %q, want %q", got, "bar/baz")
	}
}

// TestEnvUnexpandOrder ensures -x order controls unexpand precedence.
func TestEnvUnexpandOrder(t *testing.T) {
	t.Setenv("A", "foo")
	t.Setenv("B", "foobar")
	opts := options{
		envUnexpand: true,
		envOrder:    []string{"A", "B"},
		envValues: map[string]string{
			"A": "foo",
			"B": "foobar",
		},
	}

	got := transformPath("/path/foobar", opts)
	if got != "/path/$Abar" {
		t.Fatalf("transformPath env unexpand order = %q, want %q", got, "/path/$Abar")
	}
}

// TestEnvUnexpandDefaultNone ensures -E does nothing without -x.
func TestEnvUnexpandDefaultNone(t *testing.T) {
	t.Setenv("FOO", "bar")
	opts := options{
		envUnexpand: true,
	}

	got := transformPath("/path/bar", opts)
	if got != "/path/bar" {
		t.Fatalf("transformPath env unexpand default = %q, want %q", got, "/path/bar")
	}
}

// TestTildeUnexpandUser ensures -u controls unexpand output.
func TestTildeUnexpandUser(t *testing.T) {
	opts := options{
		tildeUnexpand: true,
		user:          "-",
		resolvedHome:  "/home/me",
		resolvedUser:  "",
	}

	got := transformPath("/home/me/docs", opts)
	if got != "~-/docs" {
		t.Fatalf("transformPath tilda unexpand = %q, want %q", got, "~-/docs")
	}
}

// TestRegexReplace verifies regex replacement is applied.
func TestRegexReplace(t *testing.T) {
	opts := options{
		regex:      regexp.MustCompile("aa+"),
		newPattern: "a",
	}

	got := transformPath("caaa", opts)
	if got != "ca" {
		t.Fatalf("transformPath regex = %q, want %q", got, "ca")
	}
}

// TestMakeAbsolute verifies relative paths are made absolute with a base.
func TestMakeAbsolute(t *testing.T) {
	opts := options{
		absolute: true,
		baseAbs:  "/tmp/some-dir",
	}

	got := transformPath("xxx", opts)
	if got != "/tmp/some-dir/xxx" {
		t.Fatalf("transformPath absolute = %q, want %q", got, "/tmp/some-dir/xxx")
	}
}

// TestMakeAbsoluteAlreadyAbs verifies absolute paths stay unchanged.
func TestMakeAbsoluteAlreadyAbs(t *testing.T) {
	opts := options{
		absolute: true,
		baseAbs:  "/tmp/some-dir",
	}

	got := transformPath("/tmp/foo", opts)
	if got != "/tmp/foo" {
		t.Fatalf("transformPath absolute = %q, want %q", got, "/tmp/foo")
	}
}

// TestMakeRelativePrefix verifies relative paths when base is a prefix.
func TestMakeRelativePrefix(t *testing.T) {
	opts := options{
		unabsolute:  true,
		baseAbs:     "/tmp/some-dir",
		parentLimit: 0,
		unlimitedUp: false,
	}

	got := transformPath("/tmp/some-dir/another-dir/xxx", opts)
	if got != "another-dir/xxx" {
		t.Fatalf("transformPath unabsolute = %q, want %q", got, "another-dir/xxx")
	}
}

// TestMakeRelativeParentLimit verifies parent limit behavior for non-prefix paths.
func TestMakeRelativeParentLimit(t *testing.T) {
	cases := []struct {
		name      string
		limit     int
		unlimited bool
		want      string
	}{
		{name: "limit0", limit: 0, unlimited: false, want: "/tmp/foo"},
		{name: "limit1", limit: 1, unlimited: false, want: "../foo"},
		{name: "limit2", limit: 2, unlimited: false, want: "../foo"},
		{name: "unlimited", limit: 0, unlimited: true, want: "../foo"},
	}

	for _, tc := range cases {
		opts := options{
			unabsolute:  true,
			baseAbs:     "/tmp/some-dir",
			parentLimit: tc.limit,
			unlimitedUp: tc.unlimited,
		}
		got := transformPath("/tmp/foo", opts)
		if got != tc.want {
			t.Fatalf("%s: transformPath unabsolute = %q, want %q", tc.name, got, tc.want)
		}
	}
}
