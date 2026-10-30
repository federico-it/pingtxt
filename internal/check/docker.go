package check

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// DockerHost returns the Docker Engine endpoint used for container checks:
// DOCKER_HOST (unix:// or tcp://), default unix:///var/run/docker.sock.
func DockerHost() string {
	if h := strings.TrimSpace(os.Getenv("DOCKER_HOST")); h != "" {
		return h
	}
	return "unix:///var/run/docker.sock"
}

// runDocker inspects a container on the local Docker Engine. The target is
// the container name or ID. The check fails when the container is not
// running or its healthcheck reports unhealthy.
func runDocker(ctx context.Context, j Job) Result {
	name := strings.TrimPrefix(strings.TrimSpace(j.Target), "/")
	if name == "" || strings.ContainsAny(name, "/?# ") {
		return Result{Message: "target must be a container name or id"}
	}
	host := DockerHost()
	u, err := url.Parse(host)
	if err != nil {
		return Result{Message: "invalid DOCKER_HOST"}
	}
	tr := &http.Transport{DisableKeepAlives: true}
	base := "http://docker"
	switch u.Scheme {
	case "unix":
		sock := u.Path
		tr.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		}
	case "tcp", "http":
		base = "http://" + u.Host
	case "https":
		base = "https://" + u.Host
	default:
		return Result{Message: "unsupported DOCKER_HOST " + host}
	}
	defer tr.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/containers/"+url.PathEscape(name)+"/json", nil)
	if err != nil {
		return Result{Message: err.Error()}
	}
	start := time.Now()
	resp, err := (&http.Client{Transport: tr}).Do(req)
	lat := ms(time.Since(start))
	if err != nil {
		msg := cleanErr(err)
		if strings.Contains(msg, "no such file") || strings.Contains(msg, "permission denied") {
			msg = "cannot reach docker at " + host + ": " + msg
		}
		return Result{LatencyMs: lat, Message: msg}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusNotFound {
		return Result{LatencyMs: lat, StatusCode: resp.StatusCode, Message: "container " + name + " not found"}
	}
	if resp.StatusCode != http.StatusOK {
		var e struct{ Message string }
		_ = json.Unmarshal(body, &e)
		return Result{LatencyMs: lat, StatusCode: resp.StatusCode, Message: fmt.Sprintf("docker api %d: %s", resp.StatusCode, e.Message)}
	}
	var c struct {
		State struct {
			Status     string
			Running    bool
			Paused     bool
			Restarting bool
			ExitCode   int
			StartedAt  time.Time
			Health     *struct{ Status string }
		}
	}
	if err := json.Unmarshal(body, &c); err != nil {
		return Result{LatencyMs: lat, Message: "invalid docker response"}
	}
	s := c.State
	msg := s.Status
	if s.Health != nil && s.Health.Status != "" {
		msg += ", " + s.Health.Status
	}
	switch {
	case s.Restarting:
		return Result{LatencyMs: lat, Message: "restarting (exit code " + fmt.Sprint(s.ExitCode) + ")"}
	case s.Paused:
		return Result{LatencyMs: lat, Message: msg}
	case !s.Running:
		return Result{LatencyMs: lat, Message: fmt.Sprintf("%s (exit code %d)", msg, s.ExitCode)}
	case s.Health != nil && s.Health.Status == "unhealthy":
		return Result{LatencyMs: lat, Message: msg}
	}
	if !s.StartedAt.IsZero() {
		msg += ", up " + shortDuration(time.Since(s.StartedAt))
	}
	return Result{OK: true, LatencyMs: lat, Message: msg}
}

func shortDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
