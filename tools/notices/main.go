// Command notices writes the licenses of all third-party components that
// end up in the ewiki binary (Go modules, Go standard library, fonts) into
// one text file, by default web/dist/licenses.txt. That file is embedded
// into the binary, served at /licenses.txt and shown under
// Administration → License. Run it after "go mod" changes and before
// "go build" (scripts/build.sh and the Dockerfile do).
//
// With -check it only verifies that every linked module has a license that
// is allowed in the product (see LICENSING.md) and fails otherwise (CI).
// The check also covers the npm packages of the frontend (web/package-lock.json,
// "license" of each package); packages only used for tests are listed in testOnly.
package main

import (
	"bufio"
	"bytes"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed static/*.txt
var static embed.FS

type module struct{ path, version, dir string }

func main() {
	out := flag.String("out", "web/dist/licenses.txt", "output file")
	check := flag.Bool("check", false, "only check that all licenses are allowed")
	flag.Parse()
	if err := run(*out, *check); err != nil {
		fmt.Fprintln(os.Stderr, "notices:", err)
		os.Exit(1)
	}
}

func run(out string, check bool) error {
	cmd := exec.Command("go", "list", "-deps", "-f", "{{if .Module}}{{if not .Module.Main}}{{.Module.Path}}\t{{.Module.Version}}\t{{.Module.Dir}}{{end}}{{end}}", "./cmd/ewiki")
	cmd.Stderr = os.Stderr
	b, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("go list: %w", err)
	}
	seen := map[string]module{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		if len(f) == 3 && f[0] != "" {
			seen[f[0]] = module{f[0], f[1], f[2]}
		}
	}
	mods := make([]module, 0, len(seen))
	for _, m := range seen {
		mods = append(mods, m)
	}
	sort.Slice(mods, func(i, j int) bool { return mods[i].path < mods[j].path })

	var w bytes.Buffer
	w.WriteString("Enterprise Wiki – third-party components\n")
	w.WriteString("=========================================\n\n")
	w.WriteString("Enterprise Wiki itself is licensed under the Business Source License 1.1 (see LICENSE).\n")
	w.WriteString("It contains the following third-party components under their own licenses.\n\n")
	section := func(title, text string) {
		w.WriteString(strings.Repeat("-", 78) + "\n" + title + "\n" + strings.Repeat("-", 78) + "\n\n")
		w.WriteString(strings.TrimSpace(text) + "\n\n")
	}
	goroot, _ := exec.Command("go", "env", "GOROOT").Output()
	ver, _ := exec.Command("go", "env", "GOVERSION").Output()
	if lic, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(goroot)), "LICENSE")); err == nil {
		section("Go standard library and runtime ("+strings.TrimSpace(string(ver))+")", string(lic))
	}
	var bad []string
	for _, m := range mods {
		texts := licenseFiles(m.dir)
		if len(texts) == 0 {
			return fmt.Errorf("no license file found for %s %s (%s)", m.path, m.version, m.dir)
		}
		kind := classify(texts[0])
		if !allowed[kind] {
			bad = append(bad, fmt.Sprintf("%s %s: %s", m.path, m.version, kind))
		} else if check {
			fmt.Printf("%-12s %s %s\n", kind, m.path, m.version)
		}
		section(m.path+" "+m.version+" ("+kind+")", strings.Join(texts, "\n\n"))
	}
	npmBad, err := checkNPM("web/package-lock.json", check)
	if err != nil {
		return err
	}
	bad = append(bad, npmBad...)
	if len(bad) > 0 {
		return fmt.Errorf("licenses not allowed in the product (see LICENSING.md):\n  %s", strings.Join(bad, "\n  "))
	}
	if check {
		return nil
	}
	for _, f := range []struct{ name, file string }{
		{"Inter (font, via @fontsource-variable/inter)", "static/font-inter.txt"},
		{"Source Sans 3 (font, via @fontsource-variable/source-sans-3)", "static/font-source-sans-3.txt"},
	} {
		t, _ := static.ReadFile(f.file)
		section(f.name, string(t))
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil { // #nosec G301 -- repository directory, must be readable
		return err
	}
	return os.WriteFile(out, w.Bytes(), 0o644) // #nosec G306 -- repository file (third-party notices), must be readable
}

// licenseFiles returns LICENSE, COPYING and NOTICE files of a module directory.
func licenseFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := strings.ToUpper(e.Name())
		if e.IsDir() || !(strings.HasPrefix(n, "LICENSE") || strings.HasPrefix(n, "LICENCE") || strings.HasPrefix(n, "COPYING") || strings.HasPrefix(n, "NOTICE")) {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(dir, e.Name())); err == nil { // #nosec G304 -- license files of Go modules (build tool)
			out = append(out, string(b))
		}
	}
	return out
}

// testOnly are npm packages used only by the tests (never bundled).
var testOnly = map[string]bool{"axe-core": true, "playwright": true, "playwright-core": true}

// checkNPM checks the licenses of all npm packages of the lock file. Fonts
// (SIL Open Font License) are allowed; their texts are added from static/.
func checkNPM(lock string, verbose bool) ([]string, error) {
	b, err := os.ReadFile(lock) // #nosec G304 -- package-lock.json of the repository (build tool)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var l struct {
		Packages map[string]struct {
			Version string `json:"version"`
			License any    `json:"license"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("%s: %w", lock, err)
	}
	var bad []string
	names := make([]string, 0, len(l.Packages))
	for k := range l.Packages {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if k == "" {
			continue
		}
		name := k[strings.LastIndex(k, "node_modules/")+len("node_modules/"):]
		p := l.Packages[k]
		lic, _ := p.License.(string)
		ok := allowed[lic] || lic == "OFL-1.1" || testOnly[name]
		// simple SPDX expressions: "MIT OR Apache-2.0"
		if !ok && strings.Contains(lic, " OR ") {
			for _, x := range strings.Split(strings.Trim(lic, "()"), " OR ") {
				ok = ok || allowed[strings.TrimSpace(x)]
			}
		}
		if !ok {
			bad = append(bad, fmt.Sprintf("npm %s %s: %q", name, p.Version, lic))
		} else if verbose {
			fmt.Printf("%-12s npm %s %s\n", lic, name, p.Version)
		}
	}
	return bad, nil
}

var allowed = map[string]bool{"MIT": true, "BSD-2-Clause": true, "BSD-3-Clause": true, "Apache-2.0": true, "ISC": true, "Zlib": true, "Unlicense": true, "CC0-1.0": true}

// classify recognises the common license texts (conservatively: anything
// unknown is reported and must be reviewed).
func classify(t string) string {
	t = strings.Join(strings.Fields(t), " ")
	has := func(s ...string) bool {
		for _, x := range s {
			if !strings.Contains(t, x) {
				return false
			}
		}
		return true
	}
	switch {
	case has("GNU AFFERO GENERAL PUBLIC LICENSE"):
		return "AGPL"
	case has("GNU LESSER GENERAL PUBLIC LICENSE"), has("GNU LIBRARY GENERAL PUBLIC LICENSE"):
		return "LGPL"
	case has("GNU GENERAL PUBLIC LICENSE"):
		return "GPL"
	case has("Server Side Public License"):
		return "SSPL"
	case has("Business Source License"):
		return "BUSL"
	case has("Mozilla Public License"):
		return "MPL-2.0"
	case has("Apache License", "Version 2.0"):
		return "Apache-2.0"
	case has("Permission is hereby granted, free of charge"):
		return "MIT"
	case has("Redistribution and use in source and binary forms", "Neither the name"), has("Redistribution and use in source and binary forms", "names of its contributors"):
		return "BSD-3-Clause"
	case has("Redistribution and use in source and binary forms"):
		return "BSD-2-Clause"
	case has("Permission to use, copy, modify, and/or distribute this software for any purpose"), has("Permission to use, copy, modify, and distribute this software for any purpose"):
		return "ISC"
	case has("This software is provided 'as-is'", "altered source versions must be plainly marked"):
		return "Zlib"
	case has("This is free and unencumbered software released into the public domain"):
		return "Unlicense"
	case has("CC0 1.0 Universal"):
		return "CC0-1.0"
	}
	return "unknown"
}
