package auth_ldap

import (
	"net"
	"time"
)

func dialer(timeout time.Duration) *net.Dialer { return &net.Dialer{Timeout: timeout} }
