package main

import "testing"

func TestParseStartupAddr(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{nil, ""},
		{[]string{}, ""},
		{[]string{"bken://localhost:8080"}, "localhost:8080"},
		{[]string{"--flag", "bken://10.0.0.1:8080"}, "10.0.0.1:8080"},
		{[]string{"bken://host:port/"}, "host:port"}, // trailing slash stripped
		{[]string{"bken://"}, ""},                    // empty addr → ""
		{[]string{"notbken://host:port"}, ""},        // wrong scheme
		{[]string{"someflag", "otherarg"}, ""},
	}
	for _, c := range cases {
		got := parseStartupAddr(c.args)
		if got != c.want {
			t.Errorf("parseStartupAddr(%v) = %q, want %q", c.args, got, c.want)
		}
	}
}
