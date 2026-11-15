package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/app"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// apiClient runs API requests in-process: through the normal server
// handler, so the command line gets the same permission checks, input
// checks, limits and audit entries as every other way (CLAUDE.md 1).
type apiClient struct {
	ctx context.Context
	a   *app.App
	p   *identity.Principal
}

// cliAPI is an apiClient acting as the local superuser of the command line.
func cliAPI(ctx context.Context, a *app.App) apiClient {
	return apiClient{ctx: ctx, a: a, p: identity.CLI()}
}

// do sends in as JSON (nil: no body) and decodes the answer into out
// (nil: ignored). Answers from 300 on are returned as error with the
// message of the API.
func (c apiClient) do(method, path string, in, out any) error {
	var body []byte
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = b
	}
	code, res, _ := serveInProcess(c.ctx, c.a, c.p, method, path, body, "application/json")
	if code >= 300 {
		return apiError(code, res)
	}
	if out != nil && len(bytes.TrimSpace(res)) > 0 {
		if err := json.Unmarshal(res, out); err != nil {
			return fmt.Errorf("%s %s: unexpected answer: %w", method, path, err)
		}
	}
	return nil
}

// apiErr is an error answer of the API.
type apiErr struct {
	Status  int
	Code    string // e.g. "forbidden", "external_not_accepted"
	Message string
}

func (e *apiErr) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("HTTP %d", e.Status)
	}
	return fmt.Sprintf("%s (HTTP %d)", e.Message, e.Status)
}

// apiError turns an error answer of the API into an *apiErr.
func apiError(code int, body []byte) error {
	var e struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
		return &apiErr{Status: code, Code: e.Error.Code, Message: e.Error.Message}
	}
	return &apiErr{Status: code, Message: strings.TrimSpace(string(body))}
}

// isAPICode reports whether err is an answer of the API with that code.
func isAPICode(err error, code string) bool {
	var e *apiErr
	return errors.As(err, &e) && e.Code == code
}

// lookupUser finds the id of an account by name (NAME or NAME@provider),
// e-mail address or wiki user name. Only a read to turn what people type
// into the id the API expects; every action then goes through the API.
func lookupUser(ctx context.Context, a *app.App, name string) (int64, error) {
	n, prov, _ := strings.Cut(name, "@")
	if prov == "" {
		prov = "local"
	}
	var id int64
	err := a.DB.QueryRow(ctx, `SELECT id FROM users WHERE lower(username)=lower($1) AND provider=$2`, n, prov).Scan(&id)
	if err != nil {
		err = a.DB.QueryRow(ctx, `SELECT user_id FROM user_emails WHERE email=lower($1) ORDER BY verified DESC LIMIT 1`, name).Scan(&id)
	}
	if err != nil {
		err = a.DB.QueryRow(ctx, `SELECT id FROM users WHERE lower(alias)=lower($1)`, name).Scan(&id)
	}
	if err != nil {
		return 0, fmt.Errorf("user %s not found", name)
	}
	return id, nil
}
