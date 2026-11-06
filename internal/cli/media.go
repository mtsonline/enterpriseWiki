package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"mime"
	"mime/multipart"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const mediaUsage = `usage: ewiki media list [--category ID] [--recursive] [--q TEXT] [--type image|pdf|document|video|audio]
       ewiki media upload FILE --category ID [--title T] [--alt A] [--description D] [--no-download]
                         [--watermark none|uploader_only|uploader_view|viewer_view|viewer_all]
                         [--wm-user] [--wm-date] [--wm-comment TEXT] [--acl JSON]
       ewiki media info ID | versions ID | usage ID | acl ID | delete ID
       ewiki media update ID JSON           (e.g. {"title":"…","download":false})
       ewiki media replace ID FILE [--note TEXT]
       ewiki media edit ID OPS [--note TEXT] (OPS: JSON list, e.g. [{"type":"rotate","deg":90}])
       ewiki media restore ID VERSION
       ewiki media download ID [--version N] [-o FILE]
       ewiki media viewers                  (programs: browser and active office plugins)
       ewiki media programs ID              (how the user may open the file)
       ewiki media open ID --as USER [--viewer PLUGIN] [--mode edit|view]
  --as USER acts as this user (rights, watermark with the viewer's name)`

func (e *env) media(args []string) error {
	if len(args) == 0 {
		return errors.New(mediaUsage)
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("media "+sub, flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	as := fs.String("as", "", "act as this user")
	cat := fs.Int64("category", 0, "category")
	recursive := fs.Bool("recursive", false, "include subcategories")
	q := fs.String("q", "", "search text")
	typ := fs.String("type", "", "type")
	title := fs.String("title", "", "title")
	alt := fs.String("alt", "", "alternative text")
	desc := fs.String("description", "", "description")
	noDownload := fs.Bool("no-download", false, "view only")
	wm := fs.String("watermark", "none", "watermark mode")
	wmUser := fs.Bool("wm-user", false, "watermark: name")
	wmDate := fs.Bool("wm-date", false, "watermark: date")
	wmComment := fs.String("wm-comment", "", "watermark: comment")
	aclJSON := fs.String("acl", "", "own rights (JSON list of {subject_type, subject_id, perms})")
	note := fs.String("note", "", "note on the new version")
	version := fs.Int("version", 0, "version")
	out := fs.String("o", "", "output file")
	viewer := fs.String("viewer", "", "office program (plugin id)")
	mode := fs.String("mode", "", "edit or view")
	var pos []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return err
		}
		args = fs.Args()
		if len(args) > 0 {
			pos = append(pos, args[0])
			args = args[1:]
		}
	}
	id := func() (string, error) {
		if len(pos) == 0 {
			return "", errors.New(mediaUsage)
		}
		if n, err := strconv.ParseInt(pos[0], 10, 64); err != nil || n <= 0 {
			return "", fmt.Errorf("invalid id %q", pos[0])
		}
		return pos[0], nil
	}
	switch sub {
	case "list":
		v := url.Values{}
		if *cat > 0 {
			v.Set("category", strconv.FormatInt(*cat, 10))
		}
		if *recursive {
			v.Set("recursive", "1")
		}
		if *q != "" {
			v.Set("q", *q)
		}
		if *typ != "" {
			v.Set("type", *typ)
		}
		v.Set("limit", "200")
		return e.apiBody("GET", "/api/v1/media?"+v.Encode(), nil, *as)
	case "info", "versions", "usage", "acl":
		i, err := id()
		if err != nil {
			return err
		}
		path := "/api/v1/media/" + i
		if sub != "info" {
			path += "/" + sub
		}
		return e.apiBody("GET", path, nil, *as)
	case "viewers":
		return e.apiBody("GET", "/api/v1/media/viewers", nil, *as)
	case "programs":
		i, err := id()
		if err != nil {
			return err
		}
		return e.apiBody("GET", "/api/v1/media/"+i+"/open", nil, *as)
	case "open":
		i, err := id()
		if err != nil {
			return err
		}
		body, _ := json.Marshal(map[string]string{"viewer": *viewer, "mode": *mode})
		return e.apiBody("POST", "/api/v1/media/"+i+"/open", body, *as)
	case "delete":
		i, err := id()
		if err != nil {
			return err
		}
		return e.apiBody("DELETE", "/api/v1/media/"+i, nil, *as)
	case "update":
		i, err := id()
		if err != nil || len(pos) != 2 {
			return errors.New(mediaUsage)
		}
		return e.apiBody("PATCH", "/api/v1/media/"+i, []byte(pos[1]), *as)
	case "edit":
		i, err := id()
		if err != nil || len(pos) != 2 {
			return errors.New(mediaUsage)
		}
		var ops []any
		if err := json.Unmarshal([]byte(pos[1]), &ops); err != nil {
			return fmt.Errorf("OPS: %w", err)
		}
		body, _ := json.Marshal(map[string]any{"ops": ops, "note": *note})
		return e.apiBody("POST", "/api/v1/media/"+i+"/edit", body, *as)
	case "restore":
		i, err := id()
		if err != nil || len(pos) != 2 {
			return errors.New(mediaUsage)
		}
		return e.apiBody("POST", "/api/v1/media/"+i+"/versions/"+url.PathEscape(pos[1])+"/restore", nil, *as)
	case "upload", "replace":
		var path, file string
		meta := map[string]any{}
		if sub == "upload" {
			if len(pos) != 1 || *cat <= 0 {
				return errors.New(mediaUsage)
			}
			file, path = pos[0], "/api/v1/media"
			meta = map[string]any{"category_id": *cat, "title": *title, "alt": *alt, "description": *desc, "download": !*noDownload,
				"watermark": map[string]any{"mode": *wm, "user": *wmUser, "date": *wmDate, "comment": *wmComment}}
			if *aclJSON != "" {
				var rules []any
				if err := json.Unmarshal([]byte(*aclJSON), &rules); err != nil {
					return fmt.Errorf("--acl: %w", err)
				}
				meta["acl"] = rules
			}
		} else {
			i, err := id()
			if err != nil || len(pos) != 2 {
				return errors.New(mediaUsage)
			}
			file, path = pos[1], "/api/v1/media/"+i+"/file"
			meta["note"] = *note
		}
		data, err := os.ReadFile(file) // #nosec G304 -- file named by the user on the command line
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		mj, _ := json.Marshal(meta)
		if err := mw.WriteField("meta", string(mj)); err != nil {
			return err
		}
		fw, err := mw.CreateFormFile("file", filepath.Base(file))
		if err != nil {
			return err
		}
		if _, err := fw.Write(data); err != nil {
			return err
		}
		if err := mw.Close(); err != nil {
			return err
		}
		code, body, _, err := e.callType("POST", path, buf.Bytes(), mw.FormDataContentType(), *as)
		if err != nil {
			return err
		}
		return e.printJSONResponse(code, body)
	case "download":
		i, err := id()
		if err != nil {
			return err
		}
		path := "/api/v1/media/" + i + "/download"
		if *version > 0 {
			path += "?v=" + strconv.Itoa(*version)
		}
		code, body, hdr, err := e.callType("GET", path, nil, "application/json", *as)
		if err != nil {
			return err
		}
		if code >= 400 {
			return e.printJSONResponse(code, body)
		}
		name := *out
		if name == "" {
			name = "medium-" + i
			if _, params, err := parseDisposition(hdr.Get("Content-Disposition")); err == nil && params["filename"] != "" {
				name = filepath.Base(params["filename"])
			}
		}
		if err := os.WriteFile(name, body, 0o600); err != nil {
			return err
		}
		e.printf("saved %s (%d bytes)\n", name, len(body))
		return nil
	}
	return errors.New(mediaUsage)
}

// printJSONResponse prints an API answer (pretty) and fails on errors.
func (e *env) printJSONResponse(code int, out []byte) error {
	var pretty bytes.Buffer
	if json.Indent(&pretty, out, "", "  ") == nil {
		out = append(pretty.Bytes(), '\n')
	}
	if code >= 400 {
		e.ewrite(out)
		return fmt.Errorf("HTTP %d", code)
	}
	e.print(string(out))
	return nil
}

func parseDisposition(v string) (string, map[string]string, error) {
	if strings.TrimSpace(v) == "" {
		return "", nil, errors.New("empty")
	}
	return mime.ParseMediaType(v)
}
