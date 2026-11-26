package server_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/pgversion"
)

func TestPostgresVersionNotices(t *testing.T) {
	e := setup(t)
	c := &capture{}
	e.app.Mail.SetTransport(c)
	admin := e.client()
	admin.login("admin", adminPW)
	var st struct {
		Major       int    `json:"major"`
		Recommended int    `json:"recommended"`
		Level       string `json:"level"`
		EOL         string `json:"eol"`
	}
	admin.must("GET", "/api/v1/admin/database", nil, &st, 200)
	if st.Major < 14 || st.Recommended != pgversion.RecommendedMajor || st.EOL == "" {
		t.Fatalf("status: %+v", st)
	}
	if st.Major >= st.Recommended {
		t.Skipf("test database is PostgreSQL %d (no notice expected)", st.Major)
	}
	if _, err := e.app.DB.Exec(context.Background(), `UPDATE users SET email='root@example.com' WHERE username='admin'`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // the second run sends nothing again
		if err := e.app.Workers.RunOnce(context.Background(), "pgversion"); err != nil {
			t.Fatal(err)
		}
	}
	e.flushMail(t)
	n := 0
	c.mu.Lock()
	for _, m := range c.msgs {
		if strings.Contains(decodeQP(string(m.Data)), fmt.Sprintf("PostgreSQL %d", pgversion.RecommendedMajor)) {
			n++
		}
	}
	c.mu.Unlock()
	if n != 1 {
		t.Fatalf("notice e-mails: %d", n)
	}
}
