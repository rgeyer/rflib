package buildinfo

import "testing"

func TestString(t *testing.T) {
	for _, c := range []struct {
		info Info
		want string
	}{
		{Info{Version: "v0.1.0"}, "v0.1.0"},
		{Info{Version: "dev", Commit: "1a2b3c4d5e6f7a8b9c0d1a2b3c4d5e6f7a8b9c0d"}, "dev (1a2b3c4d)"},
		{Info{Version: "dev", Commit: "1a2b3c4d5e6f7a8b9c0d1a2b3c4d5e6f7a8b9c0d-modified"}, "dev (1a2b3c4d-modified)"},
	} {
		if got := c.info.String(); got != c.want {
			t.Errorf("%+v = %q, want %q", c.info, got, c.want)
		}
	}
	if Get().Version != "dev" {
		t.Errorf("an unstamped build's version = %q", Get().Version)
	}
}
