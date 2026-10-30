package check

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func run(t *testing.T, j Job) Result {
	t.Helper()
	if j.TimeoutMs == 0 {
		j.TimeoutMs = 3000
	}
	return Run(context.Background(), j)
}

func listen(t *testing.T, serve func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); serve(c) }()
		}
	}()
	return ln.Addr().String()
}

func TestRedactTarget(t *testing.T) {
	cases := map[string]string{
		"postgres://app:s3cret@db:5432/app": "postgres://app:xxxxx@db:5432/app",
		"redis://:pw@cache:6379":            "redis://:xxxxx@cache:6379",
		"mongodb://user@db/admin":           "mongodb://user@db/admin",
		"https://example.com":               "https://example.com",
		"nginx":                             "nginx",
	}
	for in, want := range cases {
		if got := RedactTarget(in); got != want {
			t.Errorf("RedactTarget(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRedis(t *testing.T) {
	addr := listen(t, func(c net.Conn) {
		rd := bufio.NewReader(c)
		authed := false
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				return
			}
			n := 0
			for _, ch := range strings.TrimSpace(line[1:]) {
				n = n*10 + int(ch-'0')
			}
			var args []string
			for range n {
				_, _ = rd.ReadString('\n')
				a, _ := rd.ReadString('\n')
				args = append(args, strings.TrimSpace(a))
			}
			switch args[0] {
			case "AUTH":
				authed = args[len(args)-1] == "pw"
				if !authed {
					c.Write([]byte("-WRONGPASS invalid password\r\n"))
					continue
				}
				c.Write([]byte("+OK\r\n"))
			case "PING":
				if !authed {
					c.Write([]byte("-NOAUTH Authentication required.\r\n"))
					continue
				}
				c.Write([]byte("+PONG\r\n"))
			case "QUIT":
				c.Write([]byte("+OK\r\n"))
				return
			}
		}
	})
	if r := run(t, Job{Type: TypeRedis, Target: "redis://:pw@" + addr}); !r.OK || r.Message != "PONG" {
		t.Errorf("auth ok: %+v", r)
	}
	if r := run(t, Job{Type: TypeRedis, Target: "redis://:bad@" + addr}); r.OK || !strings.Contains(r.Message, "WRONGPASS") {
		t.Errorf("bad auth: %+v", r)
	}
	if r := run(t, Job{Type: TypeRedis, Target: "redis://" + addr}); r.OK || !strings.Contains(r.Message, "NOAUTH") {
		t.Errorf("no auth: %+v", r)
	}
	if r := run(t, Job{Type: TypeRedis, Target: addr}); r.OK {
		t.Errorf("bare host accepted: %+v", r)
	}
}

func TestBSONRoundTrip(t *testing.T) {
	in := bsonDoc{{"hello", int32(1)}, {"s", "x"}, {"b", true}, {"bin", []byte{1, 2}}, {"n", int64(7)}, {"f", 1.5}, {"sub", bsonDoc{{"k", "v"}}}}
	out, err := bsonDecode(bsonEncode(in))
	if err != nil {
		t.Fatal(err)
	}
	if out.num("hello") != 1 || out.str("s") != "x" || !out.bool("b") || len(out.bin("bin")) != 2 || out.num("n") != 7 || out.num("f") != 1.5 {
		t.Errorf("round trip: %#v", out)
	}
	if sub, _ := out.get("sub").(bsonDoc); sub.str("k") != "v" {
		t.Errorf("sub doc: %#v", out.get("sub"))
	}
	if _, err := bsonDecode([]byte{9, 0, 0, 0, 0x02, 'a', 0, 99, 0}); err == nil {
		t.Error("truncated document accepted")
	}
}

func TestMongoHello(t *testing.T) {
	addr := listen(t, func(c net.Conn) {
		for {
			var hdr [16]byte
			if _, err := io.ReadFull(c, hdr[:]); err != nil {
				return
			}
			rest := make([]byte, binary.LittleEndian.Uint32(hdr[:])-16)
			if _, err := io.ReadFull(c, rest); err != nil {
				return
			}
			req, _ := bsonDecode(rest[5:])
			reply := bsonDoc{{"ok", 1.0}}
			if req.has("hello") {
				reply = bsonDoc{{"isWritablePrimary", true}, {"setName", "rs0"}, {"ok", 1.0}}
			}
			body := bsonEncode(reply)
			msg := binary.LittleEndian.AppendUint32(nil, uint32(21+len(body)))
			msg = binary.LittleEndian.AppendUint32(msg, 1)
			msg = append(msg, hdr[4:8]...)
			msg = binary.LittleEndian.AppendUint32(msg, 2013)
			msg = binary.LittleEndian.AppendUint32(msg, 0)
			msg = append(msg, 0)
			c.Write(append(msg, body...))
		}
	})
	if r := run(t, Job{Type: TypeMongo, Target: "mongodb://" + addr}); !r.OK || r.Message != "ok (primary)" {
		t.Errorf("mongo: %+v", r)
	}
}

func h2cServer(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewUnstartedServer(h)
	srv.Config.Protocols = new(http.Protocols)
	srv.Config.Protocols.SetUnencryptedHTTP2(true)
	srv.Start()
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

func TestGRPCHealth(t *testing.T) {
	addr := h2cServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/grpc.health.v1.Health/Check" || r.Header.Get("Content-Type") != "application/grpc" {
			http.Error(w, "bad request", 400)
			return
		}
		buf := make([]byte, 64)
		n, _ := r.Body.Read(buf)
		service := ""
		if n > 7 {
			service = string(buf[7:n])
		}
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Trailer", "Grpc-Status")
		switch service {
		case "":
			w.Write([]byte{0, 0, 0, 0, 2, 0x08, 1}) // SERVING
			w.Header().Set("Grpc-Status", "0")
		case "down":
			w.Write([]byte{0, 0, 0, 0, 2, 0x08, 2}) // NOT_SERVING
			w.Header().Set("Grpc-Status", "0")
		default:
			w.Header().Set("Grpc-Status", "5")
		}
	})
	if r := run(t, Job{Type: TypeGRPC, Target: "grpc://" + addr}); !r.OK || r.Message != "SERVING" {
		t.Errorf("serving: %+v", r)
	}
	if r := run(t, Job{Type: TypeGRPC, Target: addr + "/down"}); r.OK || r.Message != "NOT_SERVING" {
		t.Errorf("not serving: %+v", r)
	}
	if r := run(t, Job{Type: TypeGRPC, Target: addr + "/other"}); r.OK || !strings.Contains(r.Message, "unknown") {
		t.Errorf("unknown service: %+v", r)
	}
	if r := run(t, Job{Type: TypeGRPC, Target: "grpc://host-without-port"}); r.OK {
		t.Errorf("missing port accepted: %+v", r)
	}
}

func TestSMTPStartTLS(t *testing.T) {
	tlsSrv := httptest.NewTLSServer(http.NotFoundHandler())
	cert := tlsSrv.TLS.Certificates[0]
	tlsSrv.Close()
	addr := listen(t, func(c net.Conn) {
		c.SetDeadline(time.Now().Add(3 * time.Second))
		w := func(s string) { c.Write([]byte(s + "\r\n")) }
		w("220 mx.test ESMTP ready")
		rd := bufio.NewReader(c)
		upgraded := false
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				return
			}
			switch cmd := strings.ToUpper(strings.Fields(line)[0]); cmd {
			case "EHLO":
				if upgraded {
					w("250-mx.test")
					w("250 AUTH PLAIN")
				} else {
					w("250-mx.test")
					w("250 STARTTLS")
				}
			case "STARTTLS":
				w("220 go ahead")
				tc := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{cert}})
				if tc.Handshake() != nil {
					return
				}
				c, rd, upgraded = tc, bufio.NewReader(tc), true
			case "QUIT":
				w("221 bye")
				return
			}
		}
	})
	r := run(t, Job{Type: TypeSMTP, Target: addr})
	if r.OK || !strings.Contains(r.Message, "starttls") {
		t.Errorf("untrusted cert accepted: %+v", r)
	}
	r = run(t, Job{Type: TypeSMTP, Target: "smtp://" + addr, IgnoreTLS: true})
	if !r.OK || !strings.HasPrefix(r.Message, "tls, mx.test") || r.CertExpiry == 0 {
		t.Errorf("starttls: %+v", r)
	}
}

func TestDockerSocket(t *testing.T) {
	dir := t.TempDir()
	ln, err := net.Listen("unix", dir+"/docker.sock")
	if err != nil {
		t.Skip("unix sockets unavailable:", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/containers/web/json":
			w.Write([]byte(`{"State":{"Status":"running","Running":true,"StartedAt":"2026-01-01T00:00:00Z","Health":{"Status":"healthy"}}}`))
		case "/containers/sick/json":
			w.Write([]byte(`{"State":{"Status":"running","Running":true,"Health":{"Status":"unhealthy"}}}`))
		case "/containers/dead/json":
			w.Write([]byte(`{"State":{"Status":"exited","Running":false,"ExitCode":137}}`))
		default:
			http.Error(w, `{"message":"No such container"}`, 404)
		}
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	t.Setenv("DOCKER_HOST", "unix://"+dir+"/docker.sock")

	if r := run(t, Job{Type: TypeDocker, Target: "web"}); !r.OK || !strings.HasPrefix(r.Message, "running, healthy, up ") {
		t.Errorf("web: %+v", r)
	}
	if r := run(t, Job{Type: TypeDocker, Target: "sick"}); r.OK || r.Message != "running, unhealthy" {
		t.Errorf("sick: %+v", r)
	}
	if r := run(t, Job{Type: TypeDocker, Target: "dead"}); r.OK || !strings.Contains(r.Message, "137") {
		t.Errorf("dead: %+v", r)
	}
	if r := run(t, Job{Type: TypeDocker, Target: "missing"}); r.OK || !strings.Contains(r.Message, "not found") {
		t.Errorf("missing: %+v", r)
	}
}

func TestSQLDSN(t *testing.T) {
	_, dsn, err := sqlDSN(Job{Type: TypeMySQL, Target: "mysql://u:p%40ss@db/app", TimeoutMs: 5000})
	if err != nil || !strings.HasPrefix(dsn, "u:p@ss@tcp(db:3306)/app?") || !strings.Contains(dsn, "tls=preferred") {
		t.Errorf("mysql dsn: %q %v", dsn, err)
	}
	_, dsn, err = sqlDSN(Job{Type: TypePostgres, Target: "postgresql://u:p@db/app", TimeoutMs: 5000})
	if err != nil || !strings.Contains(dsn, "sslmode=prefer") || !strings.Contains(dsn, "connect_timeout=5") {
		t.Errorf("postgres dsn: %q %v", dsn, err)
	}
	if r := run(t, Job{Type: TypePostgres, Target: "not a url"}); r.OK {
		t.Errorf("invalid target accepted: %+v", r)
	}
}
