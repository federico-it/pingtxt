package check

import (
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
)

func TestConfiguredPingMode(t *testing.T) {
	for env, want := range map[string]int32{"": pingAuto, "1": pingUnprivileged, "true": pingUnprivileged, "0": pingRaw, "false": pingRaw} {
		t.Setenv("PINGTXT_UNPRIVILEGED_PING", env)
		if got := configuredPingMode(); got != want {
			t.Errorf("PINGTXT_UNPRIVILEGED_PING=%q: mode %d, want %d", env, got, want)
		}
	}
}

func TestPermissionError(t *testing.T) {
	sock := &net.OpError{Op: "listen", Net: "ip4:icmp", Err: os.NewSyscallError("socket", syscall.EPERM)}
	if !permissionError(sock) || !permissionError(fmt.Errorf("wrapped: %w", sock)) {
		t.Error("EPERM from socket() is a permission error")
	}
	if !permissionError(os.NewSyscallError("socket", syscall.EACCES)) {
		t.Error("EACCES is a permission error")
	}
	if permissionError(&net.DNSError{Err: "no such host", Name: "x.invalid"}) {
		t.Error("a DNS error is not a permission error")
	}
}
