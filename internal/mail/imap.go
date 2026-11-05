package mail

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/tlsutil"
)

// imapClient is a minimal IMAP4rev1 client for reading the bounce mailbox
// (LOGIN, SELECT, UID SEARCH, UID FETCH, UID STORE, EXPUNGE, LOGOUT).
type imapClient struct {
	conn net.Conn
	r    *bufio.Reader
	tag  int
}

func dialIMAP(host string, port int, security, ca string, skipVerify bool, timeout time.Duration) (*imapClient, error) {
	if security == "" {
		security = "tls"
	}
	if port == 0 {
		port = 993
		if security != "tls" {
			port = 143
		}
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	d := &net.Dialer{Timeout: timeout}
	tlsCfg, err := tlsutil.Client(host, ca, skipVerify)
	if err != nil {
		return nil, err
	}
	var conn net.Conn
	if security == "tls" {
		conn, err = tls.DialWithDialer(d, "tcp", addr, tlsCfg)
	} else {
		conn, err = d.Dial("tcp", addr)
	}
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Minute))
	c := &imapClient{conn: conn, r: bufio.NewReader(conn)}
	line, err := c.r.ReadString('\n')
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if !strings.HasPrefix(line, "* OK") {
		_ = conn.Close()
		return nil, fmt.Errorf("unexpected IMAP greeting %q", strings.TrimSpace(line))
	}
	if security == "starttls" {
		if _, err := c.cmd("STARTTLS"); err != nil {
			_ = conn.Close()
			return nil, err
		}
		tc := tls.Client(conn, tlsCfg)
		if err := tc.Handshake(); err != nil {
			_ = conn.Close()
			return nil, err
		}
		c.conn, c.r = tc, bufio.NewReader(tc)
	}
	return c, nil
}

// quote returns an IMAP quoted string; line breaks and NUL are removed so a
// value cannot end the command and inject another one.
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\r", "", "\n", "", "\x00", "").Replace(s) + `"`
}

// response is an untagged response with its literals.
type response struct {
	line     string
	literals [][]byte
}

// cmd sends a command and collects the untagged responses until the tagged
// completion. Literals ({n}) are read into the response.
func (c *imapClient) cmd(format string, args ...any) ([]response, error) {
	c.tag++
	tag := fmt.Sprintf("a%03d", c.tag)
	if _, err := fmt.Fprintf(c.conn, "%s %s\r\n", tag, fmt.Sprintf(format, args...)); err != nil {
		return nil, err
	}
	var out []response
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		resp := response{}
		// read literals that continue the response
		for {
			trimmed := strings.TrimRight(line, "\r\n")
			resp.line += trimmed
			if !strings.HasSuffix(trimmed, "}") {
				break
			}
			i := strings.LastIndex(trimmed, "{")
			if i < 0 {
				break
			}
			n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSuffix(trimmed[i+1:], "}"), "+"))
			if err != nil {
				break
			}
			if n > 50<<20 {
				return nil, errors.New("IMAP literal too large")
			}
			buf := make([]byte, n)
			if _, err := io.ReadFull(c.r, buf); err != nil {
				return nil, err
			}
			resp.literals = append(resp.literals, buf)
			if line, err = c.r.ReadString('\n'); err != nil {
				return nil, err
			}
		}
		if strings.HasPrefix(resp.line, tag+" ") {
			status := strings.TrimPrefix(resp.line, tag+" ")
			if !strings.HasPrefix(status, "OK") {
				return out, fmt.Errorf("IMAP: %s", status)
			}
			return out, nil
		}
		out = append(out, resp)
	}
}

func (c *imapClient) login(user, pass string) error {
	_, err := c.cmd("LOGIN %s %s", quote(user), quote(pass))
	return err
}

func (c *imapClient) selectFolder(folder string) error {
	_, err := c.cmd("SELECT %s", quote(folder))
	return err
}

// unseen returns the UIDs of unread messages.
func (c *imapClient) unseen() ([]uint32, error) {
	res, err := c.cmd("UID SEARCH UNSEEN")
	if err != nil {
		return nil, err
	}
	var uids []uint32
	for _, r := range res {
		if !strings.HasPrefix(r.line, "* SEARCH") {
			continue
		}
		for _, f := range strings.Fields(strings.TrimPrefix(r.line, "* SEARCH")) {
			if n, err := strconv.ParseUint(f, 10, 32); err == nil {
				uids = append(uids, uint32(n))
			}
		}
	}
	return uids, nil
}

// fetch returns the complete message (without setting \Seen).
func (c *imapClient) fetch(uid uint32) ([]byte, error) {
	res, err := c.cmd("UID FETCH %d (BODY.PEEK[])", uid)
	if err != nil {
		return nil, err
	}
	for _, r := range res {
		if strings.Contains(r.line, "FETCH") && len(r.literals) > 0 {
			return r.literals[0], nil
		}
	}
	return nil, fmt.Errorf("message %d not found", uid)
}

func (c *imapClient) markSeen(uid uint32, del bool) error {
	flags := `\Seen`
	if del {
		flags += ` \Deleted`
	}
	_, err := c.cmd("UID STORE %d +FLAGS (%s)", uid, flags)
	return err
}

func (c *imapClient) expunge() error {
	_, err := c.cmd("EXPUNGE")
	return err
}

func (c *imapClient) logout() {
	_, _ = c.cmd("LOGOUT")
	_ = c.conn.Close()
}
