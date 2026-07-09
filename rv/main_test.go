package main

import "testing"

func TestRepoNameFromOriginURL(t *testing.T) {
	cases := map[string]string{
		"http://raspberrypi.local:3000/team/robot2026.git": "team/robot2026",
		"http://raspberrypi.local:3000/team/robot2026":     "team/robot2026",
		"git@raspberrypi.local:team/robot2026.git":         "team/robot2026",
		"":            "",
		"just-a-name": "",
	}
	for in, want := range cases {
		if got := repoNameFromOriginURL(in); got != want {
			t.Errorf("repoNameFromOriginURL(%q) = %q, want %q", in, got, want)
		}
	}
}
