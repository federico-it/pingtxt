package check

import (
	"bufio"
	"context"
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// procRoot is where /proc is read; tests point it elsewhere.
var procRoot = "/proc"

func readCPU() (cpuTimes, error) {
	b, err := os.ReadFile(procRoot + "/stat")
	if err != nil {
		return cpuTimes{}, err
	}
	return parseCPU(string(b), time.Now())
}

// parseCPU reads the "cpu" line of /proc/stat: user nice system idle
// iowait irq softirq steal. Idle and iowait are not busy.
func parseCPU(stat string, at time.Time) (cpuTimes, error) {
	for _, l := range strings.Split(stat, "\n") {
		f := strings.Fields(l)
		if len(f) < 5 || f[0] != "cpu" {
			continue
		}
		var total, idle uint64
		for i, s := range f[1:] {
			if i >= 8 { // guest time is already in user and nice
				break
			}
			v, err := strconv.ParseUint(s, 10, 64)
			if err != nil {
				return cpuTimes{}, errors.New("cannot read /proc/stat")
			}
			total += v
			if i == 3 || i == 4 {
				idle += v
			}
		}
		return cpuTimes{busy: total - idle, total: total, at: at}, nil
	}
	return cpuTimes{}, errors.New("cannot read /proc/stat")
}

// parseMeminfo returns MemTotal, MemAvailable, SwapTotal and SwapFree in
// bytes.
func parseMeminfo(s string) (total, avail, swapTotal, swapFree uint64, err error) {
	vals := map[string]uint64{}
	for _, l := range strings.Split(s, "\n") {
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		f := strings.Fields(v)
		if len(f) == 0 {
			continue
		}
		n, err := strconv.ParseUint(f[0], 10, 64)
		if err != nil {
			continue
		}
		if len(f) > 1 && f[1] == "kB" {
			n *= 1024
		}
		vals[k] = n
	}
	total = vals["MemTotal"]
	if total == 0 {
		return 0, 0, 0, 0, errors.New("cannot read /proc/meminfo")
	}
	avail, ok := vals["MemAvailable"]
	if !ok { // kernels before 3.14
		avail = vals["MemFree"] + vals["Buffers"] + vals["Cached"]
	}
	return total, min(avail, total), vals["SwapTotal"], min(vals["SwapFree"], vals["SwapTotal"]), nil
}

// skipFS are filesystems that are not disks.
var skipFS = map[string]bool{
	"proc": true, "sysfs": true, "tmpfs": true, "devtmpfs": true, "devpts": true, "cgroup": true, "cgroup2": true,
	"overlay": true, "squashfs": true, "nsfs": true, "tracefs": true, "debugfs": true, "securityfs": true,
	"pstore": true, "bpf": true, "autofs": true, "mqueue": true, "hugetlbfs": true, "fusectl": true,
	"configfs": true, "binfmt_misc": true, "ramfs": true, "rpc_pipefs": true, "nfsd": true, "efivarfs": true,
	"fuse.lxcfs": true, "fuse.snapfuse": true, "fuse.gvfsd-fuse": true, "fuse.portal": true, "selinuxfs": true,
	"iso9660": true, "shm": true, "zram": true,
}

var skipPrefixes = []string{"/proc", "/sys", "/dev", "/run", "/snap", "/var/lib/docker", "/var/lib/containers", "/var/lib/kubelet", "/boot/efi"}

type mountEntry struct{ dev, mount, fs string }

// parseMounts lists the disks in /proc/self/mounts: real filesystems, one
// mount per device (the shortest path), directories only, and "/" always (in a container it
// is an overlay that reports the host disk below it).
func parseMounts(s string, isDir func(string) bool) []mountEntry {
	var out []mountEntry
	seen := map[string]int{}
	for _, l := range strings.Split(s, "\n") {
		f := strings.Fields(l)
		if len(f) < 3 {
			continue
		}
		e := mountEntry{dev: f[0], mount: unescapeMount(f[1]), fs: f[2]}
		if e.mount != "/" {
			// FUSE filesystems are mostly cloud storage (rclone, s3fs) with
			// made-up sizes; fuseblk (ntfs-3g...) is a real disk.
			if strings.HasPrefix(e.fs, "fuse.") ||
				skipFS[e.fs] || !strings.HasPrefix(e.dev, "/") && !strings.Contains(e.fs, "zfs") && !strings.Contains(e.dev, ":") {
				continue
			}
			skip := false
			for _, p := range skipPrefixes {
				skip = skip || e.mount == p || strings.HasPrefix(e.mount, p+"/")
			}
			// Docker bind-mounts files such as /etc/hosts from the host disk.
			if skip || !isDir(e.mount) {
				continue
			}
		}
		if i, ok := seen[e.dev]; ok && e.mount != "/" {
			if len(e.mount) < len(out[i].mount) && out[i].mount != "/" {
				out[i] = e
			}
			continue
		}
		seen[e.dev] = len(out)
		out = append(out, e)
	}
	return out
}

// unescapeMount decodes the octal escapes of /proc/mounts (\040 = space).
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func statDisk(mount, fs string) (DiskUsage, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(mount, &st); err != nil || st.Blocks == 0 {
		return DiskUsage{}, false
	}
	bs := uint64(st.Bsize)
	total := st.Blocks * bs
	return DiskUsage{Mount: mount, FS: fs, Total: total, Used: (st.Blocks - st.Bfree) * bs, Free: st.Bavail * bs}, true
}

func collectServer(ctx context.Context, monitorID int64, mounts []string) (*ServerMetrics, error) {
	m := &ServerMetrics{Cores: runtime.NumCPU(), Disks: []DiskUsage{}}
	b, err := os.ReadFile(procRoot + "/meminfo")
	if err != nil {
		return nil, err
	}
	total, avail, swapTotal, swapFree, err := parseMeminfo(string(b))
	if err != nil {
		return nil, err
	}
	m.MemTotal, m.MemUsed, m.SwapTotal, m.SwapUsed = total, total-avail, swapTotal, swapTotal-swapFree
	if b, err := os.ReadFile(procRoot + "/loadavg"); err == nil {
		if f := strings.Fields(string(b)); len(f) >= 3 {
			m.Load1, _ = strconv.ParseFloat(f[0], 64)
			m.Load5, _ = strconv.ParseFloat(f[1], 64)
			m.Load15, _ = strconv.ParseFloat(f[2], 64)
		}
	}
	if b, err := os.ReadFile(procRoot + "/uptime"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			up, _ := strconv.ParseFloat(f[0], 64)
			m.UptimeSec = int64(up)
		}
	}
	fsOf := map[string]string{}
	var entries []mountEntry
	if f, err := os.Open(procRoot + "/self/mounts"); err == nil {
		var sb strings.Builder
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			sb.WriteString(sc.Text())
			sb.WriteByte('\n')
			if fl := strings.Fields(sc.Text()); len(fl) >= 3 {
				fsOf[unescapeMount(fl[1])] = fl[2]
			}
		}
		_ = f.Close()
		entries = parseMounts(sb.String(), func(p string) bool {
			fi, err := os.Stat(p)
			return err == nil && fi.IsDir()
		})
	}
	if len(mounts) > 0 {
		for _, p := range mounts {
			if d, ok := statDisk(p, fsOf[p]); ok {
				m.Disks = append(m.Disks, d)
			}
		}
	} else {
		for _, e := range entries {
			if d, ok := statDisk(e.mount, e.fs); ok {
				m.Disks = append(m.Disks, d)
			}
		}
	}
	if m.CPUPct, err = cpuSince(ctx, monitorID); err != nil {
		return nil, err
	}
	return m, nil
}
