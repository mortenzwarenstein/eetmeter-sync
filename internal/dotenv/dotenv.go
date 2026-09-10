// Package dotenv loads a .env file into the process environment. It parses the
// file literally — no shell expansion — so values with $, #, quotes and other
// shell metacharacters survive intact (which sourcing .env in a shell mangles).
package dotenv

import (
	"bufio"
	"os"
	"strings"
)

// Load reads path and sets any KEY=VALUE it finds that is not already present in
// the environment. A missing file is not an error. Format:
//
//	# comment lines and blank lines are ignored
//	KEY=value
//	KEY="value with spaces"     -> surrounding single/double quotes are stripped
//	export KEY=value            -> a leading "export " is ignored
//
// No variable interpolation is performed; the value is taken verbatim.
func Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")

		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if key == "" {
			continue
		}
		if _, present := os.LookupEnv(key); !present {
			_ = os.Setenv(key, val)
		}
	}
	return sc.Err()
}
