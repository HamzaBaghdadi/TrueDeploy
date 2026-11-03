// Command fixbindings rewrites relative `./x.js` imports in the
// Wails-generated TypeScript bindings to `./x`. The generator emits
// Node-ESM style paths, which Turbopack does not map to .ts files.
//
// Usage: go run ./build/tools/fixbindings [dir]   (default frontend/bindings)
package main

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Matches `from "./x.js"` and side-effect `import "./x.js"`.
var relJS = regexp.MustCompile(`((?:from|import)\s+["'])(\.{1,2}/[^"']+)\.js(["'])`)

func main() {
	dir := "frontend/bindings"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	n := 0
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".ts") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out := relJS.ReplaceAll(b, []byte("${1}${2}${3}"))
		if string(out) == string(b) {
			return nil
		}
		n++
		return os.WriteFile(p, out, 0o644)
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("fixbindings: rewrote imports in %d files", n)
}
