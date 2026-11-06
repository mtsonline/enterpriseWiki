package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/mtsonline/enterprisewiki/internal/app"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/pluginmgr"
	"github.com/mtsonline/enterprisewiki/internal/server"
	"github.com/mtsonline/enterprisewiki/internal/version"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

func printJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// api performs an in-process API request as superadmin (or as --as USER).
func (e *env) api(args []string) error {
	fs := flag.NewFlagSet("api", flag.ContinueOnError)
	as := fs.String("as", "", "act as this user (username or name@provider)")
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
	if len(pos) < 2 {
		return errors.New("usage: ewiki api METHOD PATH [BODY|@file|-] [--as USER]")
	}
	method, path := strings.ToUpper(pos[0]), pos[1]
	if !strings.HasPrefix(path, "/") {
		path = "/api/v1/" + path
	}
	var body []byte
	if len(pos) > 2 {
		switch {
		case pos[2] == "-":
			b, err := io.ReadAll(e.stdin)
			if err != nil {
				return err
			}
			body = b
		case strings.HasPrefix(pos[2], "@"):
			b, err := os.ReadFile(pos[2][1:])
			if err != nil {
				return err
			}
			body = b
		default:
			body = []byte(pos[2])
		}
	}
	return e.apiBody(method, path, body, *as)
}

// apiBody calls the API in-process (as the CLI superuser or as USER) and
// prints the answer (JSON indented).
func (e *env) apiBody(method, path string, body []byte, as string) error {
	code, out, err := e.call(method, path, body, as)
	if err != nil {
		return err
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, out, "", "  ") == nil {
		out = append(pretty.Bytes(), '\n')
	}
	if code >= 400 {
		e.ewrite(out) //nolint:errcheck
		return fmt.Errorf("HTTP %d", code)
	}
	_, err = e.stdout.Write(out)
	return err
}

// apiJSON calls the API and hands a successful answer to fn.
func (e *env) apiJSON(method, path, as string, fn func([]byte) error) error {
	code, out, err := e.call(method, path, nil, as)
	if err != nil {
		return err
	}
	if code >= 400 {
		e.ewrite(out) //nolint:errcheck
		return fmt.Errorf("HTTP %d", code)
	}
	return fn(out)
}

// call runs one API request in-process.
func (e *env) call(method, path string, body []byte, as string) (int, []byte, error) {
	code, out, _, err := e.callType(method, path, body, "application/json", as)
	return code, out, err
}

// callType runs one API request in-process with a content type (uploads)
// and returns the response headers too (downloads).
func (e *env) callType(method, path string, body []byte, contentType, as string) (int, []byte, http.Header, error) {
	var hdr http.Header
	var code int
	var out []byte
	err := e.withApp(false, func(ctx context.Context, a *app.App) error {
		p := identity.CLI()
		if as != "" {
			name, prov, _ := strings.Cut(as, "@")
			if prov == "" {
				prov = "local"
			}
			var uid int64
			if err := a.DB.QueryRow(ctx, `SELECT id FROM users WHERE lower(username)=lower($1) AND provider=$2`, name, prov).Scan(&uid); err != nil {
				return fmt.Errorf("user %s@%s not found", name, prov)
			}
			up, err := a.Auth.LoadPrincipal(ctx, uid)
			if err != nil {
				return err
			}
			// Exactly the user's rights: never the CLI superuser's.
			up.Via = "cli-as"
			p = up
		}
		srv := server.New(a, nil, pluginmgr.New(a.Root, a.Config, nil))
		req := httptest.NewRequest(method, path, bytes.NewReader(body)).WithContext(identity.With(ctx, p))
		req.Header.Set("Content-Type", contentType)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		code, out, hdr = rec.Code, rec.Body.Bytes(), rec.Header()
		return nil
	})
	return code, out, hdr, err
}

func (e *env) readPassword(fromStdin bool, prompt string) (string, error) {
	r := bufio.NewReader(e.stdin)
	if !fromStdin {
		e.eprint(prompt)
	}
	line, err := r.ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		return "", errors.New("no password given")
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (e *env) user(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ewiki user list|create|passwd|disable|enable|unlock|mfa-reset|networks")
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("user", flag.ContinueOnError)
	admin := fs.Bool("admin", false, "give the user the administrator role")
	display := fs.String("display-name", "", "display name")
	email := fs.String("email", "", "e-mail address")
	stdinPw := fs.Bool("password-stdin", false, "read the password from stdin")
	var name string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	return e.withApp(false, func(ctx context.Context, a *app.App) error {
		findID := func() (int64, error) {
			n, prov, _ := strings.Cut(name, "@")
			if prov == "" {
				prov = "local"
			}
			var id int64
			err := a.DB.QueryRow(ctx, `SELECT id FROM users WHERE lower(username)=lower($1) AND provider=$2`, n, prov).Scan(&id)
			if err != nil {
				return 0, fmt.Errorf("user %s@%s not found", n, prov)
			}
			return id, nil
		}
		switch sub {
		case "list":
			us, err := a.Auth.ListUsers(ctx, name, 500, 0)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
			rowln(tw, "ID\tHANDLE\tNAME\tE-MAIL\tDISABLED")
			for _, u := range us {
				row(tw, "%d\t%s\t%s\t%s\t%v\n", u.ID, u.Handle, u.DisplayName, u.Email, u.Disabled)
			}
			return tw.Flush()
		case "create":
			if name == "" {
				return errors.New("usage: ewiki user create NAME")
			}
			pw, err := e.readPassword(*stdinPw, "Password: ")
			if err != nil {
				return err
			}
			in := auth.NewUser{Username: name, Password: pw, DisplayName: *display, Email: *email}
			if *admin {
				rid, err := a.Auth.EnsureRole(ctx, "Administrators", "Full access to everything", true)
				if err != nil {
					return err
				}
				in.Roles = []int64{rid}
			}
			u, err := a.Auth.CreateLocalUser(ctx, in)
			if err != nil {
				return err
			}
			e.printf("created %s (id %d)\n", u.Handle, u.ID)
			return nil
		case "passwd":
			id, err := findID()
			if err != nil {
				return err
			}
			pw, err := e.readPassword(*stdinPw, "New password: ")
			if err != nil {
				return err
			}
			_, err = a.Auth.UpdateUser(ctx, id, auth.UserUpdate{Password: &pw, Unlock: true})
			if err == nil {
				e.println("password changed, sessions revoked")
			}
			return err
		case "disable", "enable":
			id, err := findID()
			if err != nil {
				return err
			}
			d := sub == "disable"
			_, err = a.Auth.UpdateUser(ctx, id, auth.UserUpdate{Disabled: &d})
			return err
		case "unlock":
			id, err := findID()
			if err != nil {
				return err
			}
			_, err = a.Auth.UpdateUser(ctx, id, auth.UserUpdate{Unlock: true})
			return err
		case "mfa-reset":
			id, err := findID()
			if err != nil {
				return err
			}
			_, err = a.Auth.UpdateUser(ctx, id, auth.UserUpdate{ResetTOTP: true})
			return err
		case "networks":
			// ewiki user networks NAME [NET...]: without networks the restriction is removed
			id, err := findID()
			if err != nil {
				return err
			}
			nets := fs.Args()
			u, err := a.Auth.UpdateUser(ctx, id, auth.UserUpdate{Networks: &nets})
			if err != nil {
				return err
			}
			if len(u.Networks) == 0 {
				e.println("no network restriction")
			}
			for _, n := range u.Networks {
				e.println(n)
			}
			return nil
		}
		return fmt.Errorf("unknown user command %q", sub)
	})
}

func (e *env) plugin(args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("plugin", flag.ContinueOnError)
	ref := fs.String("ref", "", "git tag or branch")
	build := fs.Bool("build", false, "rebuild the binary afterwards")
	var arg string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		arg, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	root := filepath.Dir(e.configPath)
	switch sub {
	case "generate":
		if err := pluginmgr.Generate(sourceRoot(root)); err != nil {
			return err
		}
		e.println("plugins/all/zz_generated.go updated")
		return nil
	case "build":
		if l := pluginmgr.LayoutFromEnv(); l.Image() {
			return imageBuild(sourceRoot(root), l, "manual", e.stdout)
		}
		return runBuild(sourceRoot(root), e.stdout, e.stderr)
	}
	return e.withApp(false, func(ctx context.Context, a *app.App) error {
		pm := pluginmgr.New(sourceRoot(a.Root), a.Config, nil)
		switch sub {
		case "list":
			tw := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
			rowln(tw, "ID\tVERSION\tACTIVE\tKINDS\tEXTENDS\tNAME")
			for _, p := range a.Kernel.List() {
				row(tw, "%s\t%s\t%v\t%s\t%s\t%s\n", p.ID, p.Version, p.Active, strings.Join(p.Kinds, ","), strings.Join(p.Extends, ","), p.Name)
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			for _, c := range pm.Pending() {
				e.printf("pending %s: %s %s (run: ewiki plugin build)\n", c.Action, c.ID, c.Version)
			}
			return nil
		case "enable", "disable":
			if arg == "" {
				return fmt.Errorf("usage: ewiki plugin %s ID", sub)
			}
			if err := a.Kernel.SetEnabled(ctx, arg, sub == "enable"); err != nil {
				return err
			}
			e.printf("%s %sd (takes effect immediately)\n", arg, sub)
			return nil
		case "install":
			if arg == "" {
				return errors.New("usage: ewiki plugin install SOURCE [--ref TAG] [--build]")
			}
			res, err := pm.Install(ctx, arg, *ref)
			if err != nil {
				return err
			}
			printJSON(e.stdout, res)
		case "updates":
			ms := []sdk.Manifest{}
			for _, p := range a.Kernel.List() {
				ms = append(ms, p.Manifest)
			}
			rep, err := pm.CheckUpdates(ctx, version.Version, ms, true)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
			rowln(tw, "ID\tCURRENT\tLATEST\tUPDATE\tNOTE")
			for _, it := range rep.Items {
				row(tw, "%s\t%s\t%s\t%v\t%s\n", it.ID, it.Current, it.Latest, it.Available, it.Error)
			}
			return tw.Flush()
		case "update":
			if arg == "" {
				return errors.New("usage: ewiki plugin update ID [--build]")
			}
			ms := []sdk.Manifest{}
			for _, p := range a.Kernel.List() {
				ms = append(ms, p.Manifest)
			}
			if _, err := pm.CheckUpdates(ctx, version.Version, ms, true); err != nil {
				return err
			}
			var it *pluginmgr.UpdateInfo
			for _, x := range pm.LastUpdateReport().Items {
				if x.ID == arg {
					x := x
					it = &x
				}
			}
			if it == nil || !it.Available {
				return fmt.Errorf("no update available for %s", arg)
			}
			res, err := pm.Install(ctx, it.Repo, it.Tag)
			if err != nil {
				return err
			}
			printJSON(e.stdout, res)
		case "remove":
			if arg == "" {
				return errors.New("usage: ewiki plugin remove ID")
			}
			if err := pm.Remove(ctx, arg); err != nil {
				return err
			}
			e.printf("%s removed from the source tree (backup in plugins/.backup)\n", arg)
		default:
			return fmt.Errorf("unknown plugin command %q", sub)
		}
		if l := pluginmgr.LayoutFromEnv(); l.Image() {
			if *build {
				if err := imageBuild(sourceRoot(a.Root), l, sub, e.stdout); err != nil {
					return err
				}
			}
			e.println("the running server notices the change, builds and restarts automatically (within about 30 seconds)")
			return nil
		}
		if *build {
			return runBuild(sourceRoot(a.Root), e.stdout, e.stderr)
		}
		e.println("rebuild required: ewiki plugin build (then restart the service)")
		return nil
	})
}

// sourceRoot finds the source tree: the config directory or EWIKI_SOURCE.
func sourceRoot(dir string) string {
	if s := os.Getenv("EWIKI_SOURCE"); s != "" {
		return s
	}
	return dir
}

func runBuild(root string, stdout, stderr io.Writer) error {
	cmd := exec.Command("bash", filepath.Join(root, "scripts", "build.sh")) // #nosec G204 -- build script of the installation
	cmd.Dir = root
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	return nil
}

var _ = http.MethodGet

// imageBuild builds the binary with the additional plugins in the full image.
func imageBuild(root string, l pluginmgr.Layout, trigger string, w io.Writer) error {
	if l.Mode == pluginmgr.ModeLite {
		return errors.New("the lite image cannot build plugins: use the full image (ghcr.io/mtsonline/enterprisewiki:full)")
	}
	if _, ok, err := pluginmgr.SourceHash(l); err != nil || !ok {
		if err != nil {
			return err
		}
		_, err := fmt.Fprintln(w, "no additional plugins in", l.Extra)
		return err
	}
	p, _, built, err := pluginmgr.BuildImage(context.Background(), root, l, trigger, w)
	if err != nil {
		return err
	}
	if !built {
		_, err = fmt.Fprintln(w, "already built:", p)
	}
	return err
}
