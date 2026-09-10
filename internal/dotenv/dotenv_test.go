package dotenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_LiteralValuesNoShellExpansion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := `
# a comment
export EXPORTED=yes
PLAIN=hello
QUOTED="has spaces and # hash"
SINGLE='literal $HOME and $(cmd)'
PASSWORD=S*uKtDywqtF3*dq$hgc#
EMPTY=
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, k := range []string{"EXPORTED", "PLAIN", "QUOTED", "SINGLE", "PASSWORD", "EMPTY", "PRESET"} {
		t.Setenv(k, "") // ensure clean, then unset the ones we want absent
		os.Unsetenv(k)
	}
	t.Setenv("PRESET", "keep-me")

	if err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	cases := map[string]string{
		"EXPORTED": "yes",
		"PLAIN":    "hello",
		"QUOTED":   "has spaces and # hash",
		"SINGLE":   "literal $HOME and $(cmd)",
		"PASSWORD": `S*uKtDywqtF3*dq$hgc#`, // the exact string a shell would mangle
		"EMPTY":    "",
		"PRESET":   "keep-me", // pre-existing env wins
	}
	for k, want := range cases {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestLoad_MissingFileIsOK(t *testing.T) {
	if err := Load(filepath.Join(t.TempDir(), "nope.env")); err != nil {
		t.Fatalf("missing file should be nil, got %v", err)
	}
}
