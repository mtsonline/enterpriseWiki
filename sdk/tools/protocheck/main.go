// Command protocheck checks the plugin contract: it compiles all .proto
// files of ewiki.plugin.v1 and reads every example manifest and every file
// of the action register with the real message descriptors (unknown fields,
// wrong enum values and wrong types are errors), and checks that every
// embedded JSON text (schemas, example bodies) is valid JSON.
//
// Run it in the sdk directory: go run ./tools/protocheck
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
	"gopkg.in/yaml.v3"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var files []string
	if err := filepath.WalkDir("proto", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".proto") {
			files = append(files, strings.TrimPrefix(p, "proto/"))
		}
		return err
	}); err != nil {
		return err
	}
	sort.Strings(files)
	comp := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{ImportPaths: []string{"proto"}}),
	}
	compiled, err := comp.Compile(context.Background(), files...)
	if err != nil {
		return fmt.Errorf("compile: %w", err)
	}
	find := func(name protoreflect.FullName) protoreflect.MessageDescriptor {
		for _, f := range compiled {
			if d := f.FindDescriptorByName(name); d != nil {
				if md, ok := d.(protoreflect.MessageDescriptor); ok {
					return md
				}
			}
		}
		return nil
	}
	manifest, action := find("ewiki.plugin.v1.Manifest"), find("ewiki.plugin.v1.Action")
	if manifest == nil || action == nil {
		return errors.New("ewiki.plugin.v1.Manifest or Action not found")
	}
	fmt.Printf("compiled %d proto files\n", len(files))

	var errs []error
	manifests, _ := filepath.Glob("proto/examples/*.plugin.yaml")
	registers, _ := filepath.Glob("proto/examples/core-register.*.yaml")
	core, _ := filepath.Glob("../internal/actions/core/*.yaml")
	plugins, _ := filepath.Glob("../plugins/*/actions.yaml")
	registers = append(append(registers, core...), plugins...)
	for _, f := range manifests {
		n, err := check(f, manifest, false)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		fmt.Printf("manifest ok: %s\n", f)
		_ = n
	}
	total := 0
	for _, f := range registers {
		n, err := check(f, action, true)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		total += n
	}
	fmt.Printf("register ok: %d files, %d actions\n", len(registers), total)
	return errors.Join(errs...)
}

// check reads a YAML file into the message (a register file: every entry
// of "actions" into Action) and returns the number of messages read.
func check(path string, md protoreflect.MessageDescriptor, register bool) (int, error) {
	b, err := os.ReadFile(path) // #nosec G304 -- files of this repository
	if err != nil {
		return 0, err
	}
	var doc any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	items := []any{doc}
	if register {
		m, _ := doc.(map[string]any)
		list, ok := m["actions"].([]any)
		if !ok || len(m) != 1 {
			return 0, fmt.Errorf("%s: a register file has exactly one key, actions (a list)", path)
		}
		items = list
	}
	var errs []error
	for i, it := range items {
		j, err := json.Marshal(it)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", path, err)
		}
		msg := dynamicpb.NewMessage(md)
		if err := (protojson.UnmarshalOptions{}).Unmarshal(j, msg); err != nil {
			errs = append(errs, fmt.Errorf("%s: entry %d: %w", path, i+1, err))
			continue
		}
		if err := embeddedJSON(msg, ""); err != nil {
			errs = append(errs, fmt.Errorf("%s: entry %d: %w", path, i+1, err))
		}
	}
	return len(items), errors.Join(errs...)
}

// embeddedJSON checks every string field named "json" or ending in "_json".
func embeddedJSON(m protoreflect.Message, at string) error {
	var errs []error
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		name := at + string(fd.Name())
		switch {
		case fd.IsList() && fd.Message() != nil:
			l := v.List()
			for i := 0; i < l.Len(); i++ {
				if err := embeddedJSON(l.Get(i).Message(), fmt.Sprintf("%s[%d].", name, i)); err != nil {
					errs = append(errs, err)
				}
			}
		case fd.IsMap():
		case fd.Message() != nil:
			if err := embeddedJSON(v.Message(), name+"."); err != nil {
				errs = append(errs, err)
			}
		case fd.Kind() == protoreflect.StringKind && (fd.Name() == "json" || strings.HasSuffix(string(fd.Name()), "_json")):
			if !json.Valid([]byte(v.String())) {
				errs = append(errs, fmt.Errorf("%s: not valid JSON", name))
			}
		}
		return true
	})
	return errors.Join(errs...)
}
