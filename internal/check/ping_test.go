package check

import (
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
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

func TestPingCount(t *testing.T) {
	for in, want := range map[int]int{0: DefaultPingCount, -2: DefaultPingCount, 1: 1, 7: 7, MaxPingCount + 5: MaxPingCount} {
		if got := (Job{PingCount: in}).pingCount(); got != want {
			t.Errorf("PingCount %d: sends %d, want %d", in, got, want)
		}
	}
	if got := MinPingTimeout(3); got != 1100*time.Millisecond {
		t.Errorf("MinPingTimeout(3) = %v", got)
	}
}
