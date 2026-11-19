// Development-only unsigned DBXP packager, following DBX checksums v1.
package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func main() {
	if len(os.Args) != 3 {
		panic("usage: package STAGE OUTPUT")
	}
	stage, out := os.Args[1], os.Args[2]
	files := map[string][]byte{}
	checksums := map[string]string{}
	err := filepath.WalkDir(stage, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("not regular: %s", p)
		}
		r, _ := filepath.Rel(stage, p)
		name := filepath.ToSlash(r)
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		h := sha256.Sum256(b)
		files[name] = b
		checksums[name] = hex.EncodeToString(h[:])
		return nil
	})
	if err != nil {
		panic(err)
	}
	files["checksums.json"], err = json.MarshalIndent(map[string]any{"algorithm": "sha256", "files": checksums}, "", "  ")
	if err != nil {
		panic(err)
	}
	f, err := os.Create(out)
	if err != nil {
		panic(err)
	}
	w := zip.NewWriter(f)
	names := []string{}
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetModTime(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
		header.SetMode(0644)
		if len(name) > 4 && name[:4] == "bin/" {
			header.SetMode(0755)
		}
		entry, e := w.CreateHeader(header)
		if e != nil {
			panic(e)
		}
		if _, e = entry.Write(files[name]); e != nil {
			panic(e)
		}
	}
	if err = w.Close(); err != nil {
		panic(err)
	}
	if err = f.Close(); err != nil {
		panic(err)
	}
	fmt.Println(out)
}
