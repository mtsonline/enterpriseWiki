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
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/app"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/kernel"
	"github.com/mtsonline/enterprisewiki/internal/pluginmgr"
	"github.com/mtsonline/enterprisewiki/internal/server"
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

// handlers are the in-process servers, one per app: a server registers its
// routes on the app's mux, which works only once.
var handlers sync.Map // *app.App -> http.Handler

// serveInProcess runs one API request against the app as principal p.
func serveInProcess(ctx context.Context, a *app.App, p *identity.Principal, method, path string, body []byte, contentType string) (int, []byte, http.Header) {
	hd, ok := handlers.Load(a)
	if !ok {
		// the plugin manager of the command line: same source tree as
		// "serve", never a build in the background (the process ends
		// with the command)
		srv := server.New(a, nil, pluginmgr.New(sourceRoot(a.Root), a.Config, nil).NoBackgroundBuild())
		hd, _ = handlers.LoadOrStore(a, srv.Handler())
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(body)).WithContext(identity.With(ctx, p))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	hd.(http.Handler).ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes(), rec.Header()
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
			uid, err := lookupUser(ctx, a, as)
			if err != nil {
				return err
			}
			up, err := a.Auth.LoadPrincipal(ctx, uid)
			if err != nil {
				return err
			}
			// Exactly the user's rights: never the CLI superuser's.
			up.Via = "cli-as"
			p = up
		}
		code, out, hdr = serveInProcess(ctx, a, p, method, path, body, contentType)
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
		return errors.New("usage: ewiki user list|create|invite|emails|email-add|email-primary|email-remove|identities|unlink|alias|sessions|logout|signin-preview|passwd|disable|enable|unlock|mfa-reset|networks")
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("user", flag.ContinueOnError)
	admin := fs.Bool("admin", false, "give the user the administrator role")
	display := fs.String("display-name", "", "display name")
	email := fs.String("email", "", "e-mail address")
	stdinPw := fs.Bool("password-stdin", false, "read the password from stdin")
	unconfirmed := fs.Bool("unconfirmed", false, "email-add: send a confirmation link instead of confirming the address")
	groups := fs.String("groups", "", "invite: groups (comma separated names); signin-preview: groups of the provider")
	verified := fs.Bool("verified", false, "signin-preview: the provider confirms the e-mail address")
	subject := fs.String("subject", "", "signin-preview: stable id at the provider (default: the user name)")
	var name string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	return e.withApp(false, func(ctx context.Context, a *app.App) error {
		c := cliAPI(ctx, a)
		findID := func() (int64, error) { return lookupUser(ctx, a, name) }
		userPath := func(id int64, rest string) string { return fmt.Sprintf("/api/v1/admin/users/%d%s", id, rest) }
		update := func(in map[string]any) error {
			id, err := findID()
			if err != nil {
				return err
			}
			return c.do("PUT", userPath(id, ""), in, nil)
		}
		switch sub {
		case "list":
			var us []auth.User
			if err := c.do("GET", "/api/v1/admin/users?limit=500&q="+url.QueryEscape(name), nil, &us); err != nil {
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
			in := map[string]any{"username": name, "password": pw, "display_name": *display, "email": *email}
			if *admin {
				rid, err := adminRole(c)
				if err != nil {
					return err
				}
				in["roles"] = []int64{rid}
			}
			var u struct {
				ID     int64  `json:"id"`
				Handle string `json:"handle"`
			}
			if err := c.do("POST", "/api/v1/admin/users", in, &u); err != nil {
				return err
			}
			e.printf("created %s (id %d)\n", u.Handle, u.ID)
			return nil
		case "invite":
			// ewiki user invite ADDRESS [--display-name N] [--groups A,B]
			gids, err := groupIDs(c, *groups)
			if err != nil {
				return err
			}
			var u struct {
				ID int64 `json:"id"`
			}
			if err := c.do("POST", "/api/v1/admin/users/invite", map[string]any{"email": name, "display_name": *display, "groups": gids}, &u); err != nil {
				return err
			}
			e.printf("invited %s (id %d)\n", name, u.ID)
			return nil
		case "emails":
			id, err := findID()
			if err != nil {
				return err
			}
			list, err := userEmails(c, id)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
			rowln(tw, "ID\tE-MAIL\tCONFIRMED\tPRIMARY")
			for _, x := range list {
				row(tw, "%d\t%s\t%v\t%v\n", x.ID, x.Email, x.Verified, x.Primary)
			}
			return tw.Flush()
		case "email-add", "email-primary", "email-remove":
			// ewiki user email-add NAME ADDRESS [--unconfirmed]; email-primary|email-remove NAME ADDRESS
			id, err := findID()
			if err != nil {
				return err
			}
			if fs.NArg() != 1 {
				return fmt.Errorf("usage: ewiki user %s NAME ADDRESS", sub)
			}
			addr := fs.Arg(0)
			if sub == "email-add" {
				return c.do("POST", userPath(id, "/emails"), map[string]any{"email": addr, "verified": !*unconfirmed}, nil)
			}
			list, err := userEmails(c, id)
			if err != nil {
				return err
			}
			for _, x := range list {
				if strings.EqualFold(x.Email, addr) {
					if sub == "email-primary" {
						return c.do("POST", userPath(id, fmt.Sprintf("/emails/%d/primary", x.ID)), nil, nil)
					}
					return c.do("DELETE", userPath(id, fmt.Sprintf("/emails/%d", x.ID)), nil, nil)
				}
			}
			return fmt.Errorf("%s has no address %s", name, addr)
		case "identities":
			id, err := findID()
			if err != nil {
				return err
			}
			var list []auth.Identity
			if err := c.do("GET", userPath(id, "/identities"), nil, &list); err != nil {
				return err
			}
			tw := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
			rowln(tw, "PROVIDER\tSUBJECT\tUSERNAME\tPRIMARY")
			for _, x := range list {
				row(tw, "%s\t%s\t%s\t%v\n", x.Provider, x.Subject, x.Username, x.Primary)
			}
			return tw.Flush()
		case "unlink":
			// ewiki user unlink NAME PROVIDER SUBJECT
			id, err := findID()
			if err != nil {
				return err
			}
			if fs.NArg() != 2 {
				return errors.New("usage: ewiki user unlink NAME PROVIDER SUBJECT")
			}
			q := url.Values{"provider": {fs.Arg(0)}, "subject": {fs.Arg(1)}}
			return c.do("DELETE", userPath(id, "/identities?"+q.Encode()), nil, nil)
		case "signin-preview":
			// ewiki user signin-preview PROVIDER USERNAME [--email E] [--verified] [--groups A,B] [--subject S]
			if fs.NArg() != 1 {
				return errors.New("usage: ewiki user signin-preview PROVIDER USERNAME [--email E] [--verified] [--groups A,B] [--subject S]")
			}
			in := auth.PreviewInput{Subject: *subject, Username: fs.Arg(0), Email: *email, EmailVerified: *verified}
			for _, g := range strings.Split(*groups, ",") {
				if g = strings.TrimSpace(g); g != "" {
					in.Groups = append(in.Groups, g)
				}
			}
			var pv json.RawMessage
			if err := c.do("POST", "/api/v1/admin/auth/providers/"+url.PathEscape(name)+"/preview", in, &pv); err != nil {
				return err
			}
			printJSON(e.stdout, pv)
			return nil
		case "alias":
			// ewiki user alias NAME [ALIAS]: set or (without ALIAS) remove the wiki user name
			id, err := findID()
			if err != nil {
				return err
			}
			return c.do("PUT", userPath(id, "/alias"), map[string]string{"alias": strings.Join(fs.Args(), "")}, nil)
		case "sessions":
			id, err := findID()
			if err != nil {
				return err
			}
			var list []auth.Session
			if err := c.do("GET", userPath(id, "/sessions"), nil, &list); err != nil {
				return err
			}
			tw := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
			rowln(tw, "ID\tKIND\tLAST SEEN\tIP\tSSO\tCLIENT")
			for _, x := range list {
				row(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", x.ID, x.Kind, x.LastSeen.Format(time.RFC3339), x.IP, x.Provider, x.UserAgent)
			}
			return tw.Flush()
		case "logout":
			// ewiki user logout NAME [SESSION-ID]: one or all sessions
			id, err := findID()
			if err != nil {
				return err
			}
			path := userPath(id, "/sessions")
			if fs.NArg() == 1 {
				sid, err := strconv.ParseInt(fs.Arg(0), 10, 64)
				if err != nil || sid <= 0 {
					return errors.New("usage: ewiki user logout NAME [SESSION-ID]")
				}
				path += "/" + strconv.FormatInt(sid, 10)
			}
			if err := c.do("DELETE", path, nil, nil); err != nil {
				return err
			}
			e.println("signed out")
			return nil
		case "passwd":
			if _, err := findID(); err != nil {
				return err
			}
			pw, err := e.readPassword(*stdinPw, "New password: ")
			if err != nil {
				return err
			}
			if err := update(map[string]any{"password": pw, "unlock": true}); err != nil {
				return err
			}
			e.println("password changed, sessions revoked")
			return nil
		case "disable", "enable":
			return update(map[string]any{"disabled": sub == "disable"})
		case "unlock":
			return update(map[string]any{"unlock": true})
		case "mfa-reset":
			return update(map[string]any{"reset_mfa": true})
		case "networks":
			// ewiki user networks NAME [NET...]: without networks the restriction is removed
			id, err := findID()
			if err != nil {
				return err
			}
			// stored by the plugin mfa.network
			var res struct {
				Networks []string `json:"networks"`
			}
			if err := c.do("PUT", "/api/v1/x/mfa.network/users/"+strconv.FormatInt(id, 10), map[string]any{"networks": fs.Args()}, &res); err != nil {
				return err
			}
			if len(res.Networks) == 0 {
				e.println("no network restriction")
			}
			for _, n := range res.Networks {
				e.println(n)
			}
			return nil
		}
		return fmt.Errorf("unknown user command %q", sub)
	})
}

// adminRole returns the id of an administrator role, created if there is
// none (first administrator from the command line).
func adminRole(c apiClient) (int64, error) {
	var roles []struct {
		ID      int64  `json:"id"`
		Name    string `json:"name"`
		IsAdmin bool   `json:"is_admin"`
	}
	if err := c.do("GET", "/api/v1/roles", nil, &roles); err != nil {
		return 0, err
	}
	for _, r := range roles {
		if r.IsAdmin && r.Name == "Administrators" {
			return r.ID, nil
		}
	}
	for _, r := range roles {
		if r.IsAdmin {
			return r.ID, nil
		}
	}
	var created struct {
		ID int64 `json:"id"`
	}
	err := c.do("POST", "/api/v1/admin/roles", map[string]any{"name": "Administrators", "description": "Full access to everything", "is_admin": true}, &created)
	return created.ID, err
}

// groupIDs turns comma separated group names into ids.
func groupIDs(c apiClient, names string) ([]int64, error) {
	var want []string
	for _, g := range strings.Split(names, ",") {
		if g = strings.TrimSpace(g); g != "" {
			want = append(want, g)
		}
	}
	if len(want) == 0 {
		return nil, nil
	}
	var all []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := c.do("GET", "/api/v1/groups", nil, &all); err != nil {
		return nil, err
	}
	var ids []int64
	for _, g := range want {
		i := slices.IndexFunc(all, func(x struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}) bool {
			return strings.EqualFold(x.Name, g)
		})
		if i < 0 {
			return nil, fmt.Errorf("group %q not found", g)
		}
		ids = append(ids, all[i].ID)
	}
	return ids, nil
}

func userEmails(c apiClient, id int64) ([]auth.Email, error) {
	var list []auth.Email
	err := c.do("GET", fmt.Sprintf("/api/v1/admin/users/%d/emails", id), nil, &list)
	return list, err
}

// externalWarning is shown before an external core plugin is installed.
const externalWarning = `Warning: this source is neither official nor entered as trusted.
The code was not reviewed by anyone and runs with full rights in the wiki:
it can read, change or pass on all content, accounts and secrets.
Install it only if you reviewed the code yourself or trust the vendor,
then repeat the command with --accept-external.`

func (e *env) plugin(args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}
	sub, args := args[0], args[1:]
	if sub == "sources" {
		return e.pluginSources(args)
	}
	fs := flag.NewFlagSet("plugin", flag.ContinueOnError)
	ref := fs.String("ref", "", "git tag or branch")
	build := fs.Bool("build", false, "rebuild the binary afterwards")
	acceptExternal := fs.Bool("accept-external", false, "confirm the warning for a source that is neither official nor trusted")
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
		c := cliAPI(ctx, a)
		external := func(err error) error {
			if isAPICode(err, "external_not_accepted") {
				e.println(externalWarning)
			}
			return err
		}
		switch sub {
		case "list":
			var res struct {
				Installed []kernel.Info      `json:"installed"`
				Pending   []pluginmgr.Change `json:"pending"`
			}
			if err := c.do("GET", "/api/v1/admin/plugins", nil, &res); err != nil {
				return err
			}
			slices.SortFunc(res.Installed, func(x, y kernel.Info) int { return strings.Compare(x.ID, y.ID) })
			tw := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
			rowln(tw, "ID\tVERSION\tACTIVE\tKINDS\tEXTENDS\tNAME")
			for _, p := range res.Installed {
				row(tw, "%s\t%s\t%v\t%s\t%s\t%s\n", p.ID, p.Version, p.Active, strings.Join(p.Kinds, ","), strings.Join(p.Extends, ","), p.Name)
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			for _, x := range res.Pending {
				e.printf("pending %s: %s %s (run: ewiki plugin build)\n", x.Action, x.ID, x.Version)
			}
			return nil
		case "enable", "disable":
			if arg == "" {
				return fmt.Errorf("usage: ewiki plugin %s ID", sub)
			}
			if err := c.do("POST", "/api/v1/admin/plugins/"+url.PathEscape(arg)+"/"+sub, nil, nil); err != nil {
				return err
			}
			e.printf("%s %sd (takes effect immediately)\n", arg, sub)
			return nil
		case "install":
			if arg == "" {
				return errors.New("usage: ewiki plugin install SOURCE [--ref TAG] [--build] [--accept-external]")
			}
			var res json.RawMessage
			if err := external(c.do("POST", "/api/v1/admin/plugins/install", map[string]any{"source": arg, "ref": *ref, "accept_external": *acceptExternal}, &res)); err != nil {
				return err
			}
			printJSON(e.stdout, res)
		case "updates":
			var rep pluginmgr.UpdateReport
			if err := c.do("GET", "/api/v1/admin/updates?force=1", nil, &rep); err != nil {
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
			var res json.RawMessage
			if err := external(c.do("POST", "/api/v1/admin/updates/"+url.PathEscape(arg), map[string]any{"accept_external": *acceptExternal}, &res)); err != nil {
				return err
			}
			printJSON(e.stdout, res)
		case "remove":
			if arg == "" {
				return errors.New("usage: ewiki plugin remove ID")
			}
			if err := c.do("DELETE", "/api/v1/admin/plugins/"+url.PathEscape(arg), nil, nil); err != nil {
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
