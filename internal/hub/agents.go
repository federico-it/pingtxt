package hub

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/protocol"
	"github.com/federico-it/pingtxt/internal/store"
)

type agentConn struct {
	agent *store.Agent
	hello protocol.Hello
	conn  *websocket.Conn
	out   chan protocol.Message
	once  sync.Once
	done  chan struct{}
	ipv6  atomic.Int32 // check.IPv6Yes/No as reported, IPv6Unknown before
}

func (a *agentConn) label() string {
	return agentLabel(a.agent)
}

// agentLabel is how an agent is shown as a monitor location: its name,
// followed by its location when one is set.
func agentLabel(a *store.Agent) string {
	switch {
	case a.Location == "" || a.Location == a.Name:
		return a.Name
	case a.Name == "":
		return a.Location
	}
	return a.Name + " · " + a.Location
}

func (a *agentConn) send(m protocol.Message) {
	select {
	case a.out <- m:
	case <-a.done:
	default:
		// Queue full: the agent is too slow, drop the job.
	}
}

func (a *agentConn) close() {
	a.once.Do(func() {
		close(a.done)
		_ = a.conn.Close(websocket.StatusNormalClosure, "bye")
	})
}

// NewToken returns a random agent token and its hash.
func NewToken(prefix string) (token, hash string) {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	token = prefix + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
	return token, HashToken(token)
}

// HashToken hashes a secret token for storage.
func HashToken(t string) string {
	s := sha256.Sum256([]byte(t))
	return hex.EncodeToString(s[:])
}

func bearer(r *http.Request) string {
	if v := r.Header.Get("Authorization"); strings.HasPrefix(v, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(v, "Bearer "))
	}
	return ""
}

func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		ip, _, _ := strings.Cut(v, ",")
		return strings.TrimSpace(ip)
	}
	if v := r.Header.Get("X-Real-Ip"); v != "" {
		return v
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ServeAgent handles the agent WebSocket endpoint.
func (h *Hub) ServeAgent(w http.ResponseWriter, r *http.Request) {
	token := bearer(r)
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	ag, err := h.store.AgentByToken(HashToken(token))
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	conn.SetReadLimit(1 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Handshake.
	hctx, hcancel := context.WithTimeout(ctx, 15*time.Second)
	var hello protocol.Message
	err = wsjson.Read(hctx, conn, &hello)
	hcancel()
	if err != nil || hello.Type != protocol.TypeHello || hello.Hello == nil {
		conn.Close(websocket.StatusPolicyViolation, "expected hello")
		return
	}
	hl := hello.Hello
	_ = h.store.TouchAgent(ag.ID, hl.Version, hl.Hostname, hl.OS+"/"+hl.Arch, clientIP(r))

	ac := &agentConn{agent: ag, hello: *hl, conn: conn, out: make(chan protocol.Message, 256), done: make(chan struct{})}
	ac.ipv6.Store(int32(ag.IPv6)) // last known, until the agent reports again
	h.mu.Lock()
	if old := h.agents[ag.ID]; old != nil {
		old.close()
	}
	h.agents[ag.ID] = ac
	h.mu.Unlock()
	h.log.Info("agent connected", "agent", ag.Name, "addr", clientIP(r), "version", hl.Version)

	defer func() {
		h.mu.Lock()
		if h.agents[ag.ID] == ac {
			delete(h.agents, ag.ID)
			// Forget this location's state so it does not affect status while offline.
			loc := AgentLocation(ag.ID)
			for _, st := range h.monitors {
				delete(st.locs, loc)
			}
		}
		h.mu.Unlock()
		_ = h.store.AgentSeen(ag.ID)
		ac.close()
		h.log.Info("agent disconnected", "agent", ag.Name)
	}()

	_ = wsjson.Write(ctx, conn, protocol.Message{Type: protocol.TypeWelcome, Welcome: &protocol.Welcome{AgentID: ag.ID, Name: ag.Name, Location: ag.Location}})

	// Writer + keepalive.
	go func() {
		ping := time.NewTicker(30 * time.Second)
		defer ping.Stop()
		for {
			select {
			case <-ac.done:
				return
			case m := <-ac.out:
				wctx, c := context.WithTimeout(ctx, 10*time.Second)
				err := wsjson.Write(wctx, conn, m)
				c()
				if err != nil {
					ac.close()
					return
				}
			case <-ping.C:
				pctx, c := context.WithTimeout(ctx, 15*time.Second)
				err := conn.Ping(pctx)
				c()
				if err != nil {
					ac.close()
					return
				}
				_ = h.store.AgentSeen(ag.ID)
			}
		}
	}()

	h.agentConnected(ac)

	loc := AgentLocation(ag.ID)
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var msg protocol.Message
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch {
		case msg.Type == protocol.TypeResult && msg.Result != nil:
			if !h.tests.deliver(*msg.Result) {
				h.HandleResult(loc, *msg.Result)
			}
		case msg.Type == protocol.TypeNetwork && msg.Network != nil:
			v := check.IPv6No
			if msg.Network.IPv6 {
				v = check.IPv6Yes
			}
			if ac.ipv6.Swap(v) != v {
				h.log.Info("agent IPv6", "agent", ag.Name, "available", msg.Network.IPv6)
				_ = h.store.SetAgentIPv6(ag.ID, int(v))
			}
		case msg.Type == protocol.TypeUpdateStatus && msg.UpdateStatus != nil:
			h.log.Warn("agent update failed", "agent", ag.Name, "err", msg.UpdateStatus.Error)
			h.updateFailed(ag.ID, *msg.UpdateStatus)
		}
	}
}

// DisconnectAgent closes an agent's connection, if any.
func (h *Hub) DisconnectAgent(id int64) {
	h.mu.Lock()
	ac := h.agents[id]
	h.mu.Unlock()
	if ac != nil {
		ac.close()
	}
}
