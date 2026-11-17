package pluginmgr

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/config"
)

// DefaultCredentialUser is the user name sent with a token when none is set
// (GitHub expects it for tokens; other hosts ignore or need their own).
const DefaultCredentialUser = "x-access-token"

// NormalizeCredentialPrefix checks and normalises the address prefix of
// source credentials: https only (a token never travels unencrypted), a
// host, optionally a path; no user, query or fragment.
func NormalizeCredentialPrefix(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "https") {
		return "", errors.New("access data need an https address (https://host/path)")
	}
	if u.User != nil {
		return "", errors.New("the address must not contain a user or password; enter them in their own fields")
	}
	norm, err := NormalizeSource(raw)
	if err != nil {
		return "", err
	}
	return norm, nil
}

// SetCredential adds or replaces (same prefix) the access data in c.
func SetCredential(c *config.Config, in config.SourceCredential) (config.SourceCredential, error) {
	prefix, err := NormalizeCredentialPrefix(in.Prefix)
	if err != nil {
		return in, apperr.BadRequest("%s", err.Error())
	}
	in.Prefix = prefix
	in.Username = strings.TrimSpace(in.Username)
	in.Note = strings.TrimSpace(in.Note)
	if strings.ContainsAny(in.Username, ":\r\n") {
		return in, apperr.BadRequest("the user name must not contain a colon or line break")
	}
	if strings.TrimSpace(in.Token) == "" {
		return in, apperr.BadRequest("token is required")
	}
	if strings.ContainsAny(in.Token, "\r\n") {
		return in, apperr.BadRequest("the token must not contain a line break")
	}
	out := make([]config.SourceCredential, 0, len(c.Plugins.Credentials)+1)
	for _, x := range c.Plugins.Credentials {
		if x.Prefix != prefix {
			out = append(out, x)
		}
	}
	c.Plugins.Credentials = append(out, in)
	return in, nil
}

// credentialFor returns the access data whose prefix covers the source
// (the longest prefix wins). Only https sources get access data.
func credentialFor(list []config.SourceCredential, source string) (config.SourceCredential, bool) {
	if !strings.HasPrefix(strings.ToLower(source), "https://") {
		return config.SourceCredential{}, false
	}
	norm, err := NormalizeSource(source)
	if err != nil {
		return config.SourceCredential{}, false
	}
	var best config.SourceCredential
	found := false
	for _, c := range list {
		if c.Token != "" && prefixMatches(norm, c.Prefix) && len(c.Prefix) > len(best.Prefix) {
			best, found = c, true
		}
	}
	return best, found
}

// gitAuthEnv returns the environment for git that sends the access data to
// the cloned address (below the prefix) only: as a header in the
// environment, never
// in the command line or the address (where logs, error messages and the
// process list would show it). Redirects are not followed and credential
// helpers of the server are not asked.
func gitAuthEnv(cred config.SourceCredential, gitURL string) []string {
	user := cred.Username
	if user == "" {
		user = DefaultCredentialUser
	}
	header := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+cred.Token))
	return []string{
		"GIT_CONFIG_COUNT=3",
		// the address git fetches (with ".git"): a prefix naming the
		// repository itself would not match it at a path boundary
		"GIT_CONFIG_KEY_0=http." + strings.TrimSuffix(gitURL, "/") + "/.extraHeader", "GIT_CONFIG_VALUE_0=" + header,
		"GIT_CONFIG_KEY_1=http.followRedirects", "GIT_CONFIG_VALUE_1=false",
		"GIT_CONFIG_KEY_2=credential.helper", "GIT_CONFIG_VALUE_2=",
	}
}

// redact removes the token (and its encoded form) from text that may be shown.
func redact(text string, cred config.SourceCredential) string {
	if cred.Token == "" {
		return text
	}
	user := cred.Username
	if user == "" {
		user = DefaultCredentialUser
	}
	for _, s := range []string{cred.Token, base64.StdEncoding.EncodeToString([]byte(user + ":" + cred.Token))} {
		text = strings.ReplaceAll(text, s, "***")
	}
	return text
}

// CredentialView is what the API shows of access data: never the token.
type CredentialView struct {
	Prefix   string `json:"prefix"`
	Username string `json:"username"`
	Note     string `json:"note,omitempty"`
	TokenSet bool   `json:"token_set"`
	FromEnv  bool   `json:"from_env"`
}

// ViewCredential describes access data without the token. raw is the
// stored (not expanded) entry.
func ViewCredential(raw config.SourceCredential) CredentialView {
	user := raw.Username
	if user == "" {
		user = DefaultCredentialUser
	}
	return CredentialView{Prefix: raw.Prefix, Username: user, Note: raw.Note, TokenSet: raw.Token != "", FromEnv: config.HasEnvRef(raw.Token)}
}

func (c CredentialView) String() string { return fmt.Sprintf("%s (%s)", c.Prefix, c.Username) }
