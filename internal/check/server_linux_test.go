package check

import (
	"context"
	"testing"
	"time"
)

func TestParseCPU(t *testing.T) {
	c, err := parseCPU("cpu  100 0 50 800 50 0 0 0 0 0\ncpu0 1 2 3 4\n", time.Now())
	if err != nil || c.total != 1000 || c.busy != 150 {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestParseMeminfo(t *testing.T) {
	total, avail, st, sf, err := parseMeminfo("MemTotal:  2048 kB\nMemFree: 100 kB\nMemAvailable: 1024 kB\nSwapTotal: 512 kB\nSwapFree: 512 kB\n")
	if err != nil || total != 2048*1024 || avail != 1024*1024 || st != 512*1024 || sf != st {
		t.Fatalf("%d %d %d %d %v", total, avail, st, sf, err)
	}
}

func TestParseMounts(t *testing.T) {
	got := parseMounts(`overlay / overlay rw 0 0
proc /proc proc rw 0 0
tmpfs /dev tmpfs rw 0 0
/dev/sda1 /etc/hosts ext4 rw 0 0
/dev/sda1 /var/lib/pingtxt ext4 rw 0 0
/dev/sdb1 /mnt/my\040disk xfs rw 0 0
/dev/sdb1 /mnt/my\040disk/sub xfs rw 0 0
/dev/loop0 /snap/core/1 squashfs ro 0 0
nas:/export /mnt/nas nfs4 rw 0 0
remote: /mnt/cloud fuse.rclone rw 0 0
`, func(p string) bool { return p != "/etc/hosts" })
	want := []string{"/", "/var/lib/pingtxt", "/mnt/my disk", "/mnt/nas"}
	if len(got) != len(want) {
		t.Fatalf("%+v", got)
	}
	for i, w := range want {
		if got[i].mount != w {
			t.Errorf("%d: %q want %q", i, got[i].mount, w)
		}
	}
}

func TestServerCheckHere(t *testing.T) {
	r := Run(context.Background(), Job{Type: TypeServer, MonitorID: 99, TimeoutMs: 5000})
	if !r.OK || r.Server == nil || r.Server.MemTotal == 0 || r.Server.Cores == 0 {
		t.Fatalf("%+v", r)
	}
	t.Log(r.Message, len(r.Server.Disks), "disks")
}
