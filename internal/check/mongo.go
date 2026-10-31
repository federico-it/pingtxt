package check

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5" //nolint:gosec // required by SCRAM-SHA-1 for MongoDB
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // required by SCRAM-SHA-1
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"net"
	"net/url"
	"strings"
	"time"
)

// runMongo connects to MongoDB with a hand-written OP_MSG client (the
// official driver would add several MB to the agent), authenticates with
// SCRAM when the URL has credentials, and runs ping.
//
//	mongodb://[user:pass@]host[:27017][/db][?authSource=admin&tls=true]
//	mongodb+srv://user:pass@cluster.example.net/db
func runMongo(ctx context.Context, j Job) Result {
	u, err := ParseMongoURL(j.Target)
	if err != nil {
		return Result{Message: err.Error()}
	}
	q := u.Query()
	useTLS := q.Get("tls") == "true" || q.Get("ssl") == "true"
	host := u.Host
	start := time.Now()
	var addr string
	if u.Scheme == "mongodb+srv" {
		if q.Get("tls") != "false" && q.Get("ssl") != "false" {
			useTLS = true
		}
		_, srvs, err := net.DefaultResolver.LookupSRV(ctx, "mongodb", "tcp", u.Hostname())
		if err != nil || len(srvs) == 0 {
			return Result{LatencyMs: ms(time.Since(start)), Message: "srv lookup: " + cleanErr(err)}
		}
		addr = net.JoinHostPort(strings.TrimSuffix(srvs[0].Target, "."), fmt.Sprint(srvs[0].Port))
	} else if _, _, err := net.SplitHostPort(host); err == nil {
		addr = host
	} else {
		addr = net.JoinHostPort(strings.Trim(host, "[]"), "27017")
	}
	serverName, _, _ := net.SplitHostPort(addr)
	conn, certExpiry, err := dial(ctx, j, addr, useTLS, serverName, j.IgnoreTLS || q.Get("tlsInsecure") == "true")
	if err != nil {
		return Result{LatencyMs: ms(time.Since(start)), Message: cleanErr(err)}
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	mc := &mongoConn{rw: conn}
	fail := func(err error) Result {
		if ctx.Err() != nil {
			err = context.DeadlineExceeded
		}
		return Result{LatencyMs: ms(time.Since(start)), CertExpiry: certExpiry, Message: cleanErr(err)}
	}

	hello, err := mc.command("admin", bsonDoc{{"hello", int32(1)}})
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "no such") {
		hello, err = mc.command("admin", bsonDoc{{"isMaster", int32(1)}}) // MongoDB < 4.4
	}
	if err != nil {
		return fail(err)
	}
	role := "standalone"
	switch {
	case hello.bool("isWritablePrimary") || hello.bool("ismaster"):
		if hello.has("setName") {
			role = "primary"
		}
	case hello.bool("secondary"):
		role = "secondary"
	case hello.bool("arbiterOnly"):
		role = "arbiter"
	default:
		if hello.has("setName") {
			role = "not primary"
		}
	}
	if hello.str("msg") == "isdbgrid" {
		role = "mongos"
	}

	if user := u.User.Username(); user != "" {
		pass, _ := u.User.Password()
		authDB := q.Get("authSource")
		if authDB == "" {
			authDB = strings.TrimPrefix(u.Path, "/")
		}
		if authDB == "" {
			authDB = "admin"
		}
		mech := q.Get("authMechanism")
		if mech == "" {
			mech = "SCRAM-SHA-256"
		}
		if err := mc.scram(authDB, mech, user, pass); err != nil {
			return fail(fmt.Errorf("auth: %w", err))
		}
	}
	db := strings.TrimPrefix(u.Path, "/")
	if db == "" {
		db = "admin"
	}
	if _, err := mc.command(db, bsonDoc{{"ping", int32(1)}}); err != nil {
		return fail(err)
	}
	return Result{OK: true, LatencyMs: ms(time.Since(start)), CertExpiry: certExpiry, Message: "ok (" + role + ")"}
}

// ParseMongoURL parses a MongoDB connection string. Of a seed list
// (host1,host2,...) only the first host is kept: it is enough for a health
// check.
func ParseMongoURL(target string) (*url.URL, error) {
	errInvalid := errors.New("target must be a mongodb:// or mongodb+srv:// URL")
	scheme, rest, ok := strings.Cut(strings.TrimSpace(target), "://")
	if !ok || (scheme != "mongodb" && scheme != "mongodb+srv") {
		return nil, errInvalid
	}
	end := strings.IndexAny(rest, "/?")
	if end < 0 {
		end = len(rest)
	}
	authority, tail := rest[:end], rest[end:]
	userinfo := ""
	if i := strings.LastIndex(authority, "@"); i >= 0 {
		userinfo, authority = authority[:i+1], authority[i+1:]
	}
	authority = strings.Split(authority, ",")[0]
	u, err := url.Parse(scheme + "://" + userinfo + authority + tail)
	if err != nil || u.Host == "" {
		return nil, errInvalid
	}
	return u, nil
}

type mongoConn struct {
	rw     io.ReadWriter
	nextID int32
}

// command sends an OP_MSG and returns the reply document. A reply with
// ok != 1 is returned as an error carrying the server's errmsg.
func (c *mongoConn) command(db string, cmd bsonDoc) (bsonDoc, error) {
	cmd = append(cmd, bsonElem{"$db", db})
	body := bsonEncode(cmd)
	c.nextID++
	msg := make([]byte, 0, 21+len(body))
	msg = binary.LittleEndian.AppendUint32(msg, uint32(21+len(body)))
	msg = binary.LittleEndian.AppendUint32(msg, uint32(c.nextID))
	msg = binary.LittleEndian.AppendUint32(msg, 0)
	msg = binary.LittleEndian.AppendUint32(msg, 2013) // OP_MSG
	msg = binary.LittleEndian.AppendUint32(msg, 0)    // flags
	msg = append(msg, 0)                              // section kind 0: body
	msg = append(msg, body...)
	if _, err := c.rw.Write(msg); err != nil {
		return nil, err
	}
	var hdr [16]byte
	if _, err := io.ReadFull(c.rw, hdr[:]); err != nil {
		return nil, err
	}
	size := binary.LittleEndian.Uint32(hdr[:4])
	if size < 21 || size > 16<<20 {
		return nil, errors.New("invalid reply size")
	}
	rest := make([]byte, size-16)
	if _, err := io.ReadFull(c.rw, rest); err != nil {
		return nil, err
	}
	if op := binary.LittleEndian.Uint32(hdr[12:]); op != 2013 {
		return nil, fmt.Errorf("unexpected opcode %d", op)
	}
	if rest[4] != 0 {
		return nil, errors.New("unexpected reply section")
	}
	doc, err := bsonDecode(rest[5:])
	if err != nil {
		return nil, err
	}
	if doc.num("ok") != 1 {
		if m := doc.str("errmsg"); m != "" {
			return doc, errors.New(m)
		}
		return doc, errors.New("command failed")
	}
	return doc, nil
}

// scram runs SCRAM-SHA-256 or SCRAM-SHA-1 (RFC 5802) over saslStart and
// saslContinue.
func (c *mongoConn) scram(db, mech, user, pass string) error {
	var newHash func() hash.Hash
	switch mech {
	case "SCRAM-SHA-256":
		newHash = sha256.New
	case "SCRAM-SHA-1":
		newHash = sha1.New
		sum := md5.Sum([]byte(user + ":mongo:" + pass)) //nolint:gosec // protocol-defined
		pass = hex.EncodeToString(sum[:])
	default:
		return errors.New("unsupported authMechanism " + mech)
	}
	nonceRaw := make([]byte, 24)
	_, _ = rand.Read(nonceRaw)
	nonce := base64.StdEncoding.EncodeToString(nonceRaw)
	escUser := strings.NewReplacer("=", "=3D", ",", "=2C").Replace(user)
	clientFirstBare := "n=" + escUser + ",r=" + nonce
	res, err := c.command(db, bsonDoc{
		{"saslStart", int32(1)},
		{"mechanism", mech},
		{"payload", []byte("n,," + clientFirstBare)},
		{"options", bsonDoc{{"skipEmptyExchange", true}}},
	})
	if err != nil {
		return err
	}
	serverFirst := string(res.bin("payload"))
	attrs := scramAttrs(serverFirst)
	if !strings.HasPrefix(attrs["r"], nonce) {
		return errors.New("server nonce mismatch")
	}
	salt, err := base64.StdEncoding.DecodeString(attrs["s"])
	if err != nil {
		return errors.New("invalid salt")
	}
	var iter int
	if _, err := fmt.Sscan(attrs["i"], &iter); err != nil || iter < 1 {
		return errors.New("invalid iteration count")
	}
	salted, err := pbkdf2.Key(newHash, pass, salt, iter, newHash().Size())
	if err != nil {
		return err
	}
	mac := func(key []byte, s string) []byte {
		m := hmac.New(newHash, key)
		m.Write([]byte(s))
		return m.Sum(nil)
	}
	clientKey := mac(salted, "Client Key")
	h := newHash()
	h.Write(clientKey)
	storedKey := h.Sum(nil)
	withoutProof := "c=biws,r=" + attrs["r"]
	authMsg := clientFirstBare + "," + serverFirst + "," + withoutProof
	sig := mac(storedKey, authMsg)
	proof := make([]byte, len(clientKey))
	for i := range proof {
		proof[i] = clientKey[i] ^ sig[i]
	}
	serverSig := mac(mac(salted, "Server Key"), authMsg)
	convID := res.get("conversationId")
	res, err = c.command(db, bsonDoc{
		{"saslContinue", int32(1)},
		{"conversationId", convID},
		{"payload", []byte(withoutProof + ",p=" + base64.StdEncoding.EncodeToString(proof))},
	})
	if err != nil {
		return err
	}
	final := scramAttrs(string(res.bin("payload")))
	if e := final["e"]; e != "" {
		return errors.New(e)
	}
	if v, _ := base64.StdEncoding.DecodeString(final["v"]); !hmac.Equal(v, serverSig) {
		return errors.New("invalid server signature")
	}
	for i := 0; !res.bool("done") && i < 2; i++ {
		res, err = c.command(db, bsonDoc{
			{"saslContinue", int32(1)},
			{"conversationId", convID},
			{"payload", []byte{}},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func scramAttrs(s string) map[string]string {
	out := map[string]string{}
	for _, p := range strings.Split(s, ",") {
		if k, v, ok := strings.Cut(p, "="); ok && len(k) == 1 {
			out[k] = v
		}
	}
	return out
}

// ---------- minimal BSON ----------

type bsonElem struct {
	key string
	val any
}

type bsonDoc []bsonElem

func (d bsonDoc) get(k string) any {
	for _, e := range d {
		if e.key == k {
			return e.val
		}
	}
	return nil
}

func (d bsonDoc) has(k string) bool { return d.get(k) != nil }

func (d bsonDoc) bool(k string) bool { b, _ := d.get(k).(bool); return b }

func (d bsonDoc) str(k string) string { s, _ := d.get(k).(string); return s }

func (d bsonDoc) bin(k string) []byte { b, _ := d.get(k).([]byte); return b }

func (d bsonDoc) num(k string) float64 {
	switch v := d.get(k).(type) {
	case float64:
		return v
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case bool:
		if v {
			return 1
		}
	}
	return 0
}

// bsonEncode supports the value types the probe sends.
func bsonEncode(d bsonDoc) []byte {
	var b bytes.Buffer
	b.Write([]byte{0, 0, 0, 0})
	for _, e := range d {
		switch v := e.val.(type) {
		case string:
			b.WriteByte(0x02)
			b.WriteString(e.key)
			b.WriteByte(0)
			_ = binary.Write(&b, binary.LittleEndian, int32(len(v)+1))
			b.WriteString(v)
			b.WriteByte(0)
		case bsonDoc:
			b.WriteByte(0x03)
			b.WriteString(e.key)
			b.WriteByte(0)
			b.Write(bsonEncode(v))
		case []byte:
			b.WriteByte(0x05)
			b.WriteString(e.key)
			b.WriteByte(0)
			_ = binary.Write(&b, binary.LittleEndian, int32(len(v)))
			b.WriteByte(0) // generic subtype
			b.Write(v)
		case bool:
			b.WriteByte(0x08)
			b.WriteString(e.key)
			b.WriteByte(0)
			if v {
				b.WriteByte(1)
			} else {
				b.WriteByte(0)
			}
		case int32:
			b.WriteByte(0x10)
			b.WriteString(e.key)
			b.WriteByte(0)
			_ = binary.Write(&b, binary.LittleEndian, v)
		case int64:
			b.WriteByte(0x12)
			b.WriteString(e.key)
			b.WriteByte(0)
			_ = binary.Write(&b, binary.LittleEndian, v)
		case float64:
			b.WriteByte(0x01)
			b.WriteString(e.key)
			b.WriteByte(0)
			_ = binary.Write(&b, binary.LittleEndian, math.Float64bits(v))
		}
	}
	b.WriteByte(0)
	out := b.Bytes()
	binary.LittleEndian.PutUint32(out, uint32(len(out)))
	return out
}

var errBSON = errors.New("invalid bson")

// bsonDecode decodes the top level of a document. Nested documents are
// decoded too; other types the probe does not need are skipped.
func bsonDecode(b []byte) (bsonDoc, error) {
	if len(b) < 5 {
		return nil, errBSON
	}
	size := int(binary.LittleEndian.Uint32(b))
	if size < 5 || size > len(b) {
		return nil, errBSON
	}
	b = b[4 : size-1]
	var d bsonDoc
	for len(b) > 0 {
		t := b[0]
		end := bytes.IndexByte(b[1:], 0)
		if end < 0 {
			return nil, errBSON
		}
		key := string(b[1 : 1+end])
		b = b[2+end:]
		var val any
		n := 0
		fixed := func(size int) bool { n = size; return len(b) >= size }
		ok := true
		switch t {
		case 0x01: // double
			if ok = fixed(8); ok {
				val = math.Float64frombits(binary.LittleEndian.Uint64(b))
			}
		case 0x02, 0x0D, 0x0E: // string, js code, symbol
			if ok = len(b) >= 4; ok {
				l := int(int32(binary.LittleEndian.Uint32(b)))
				if ok = l >= 1 && len(b) >= 4+l; ok {
					val, n = string(b[4:4+l-1]), 4+l
				}
			}
		case 0x03, 0x04: // document, array
			if ok = len(b) >= 4; ok {
				l := int(int32(binary.LittleEndian.Uint32(b)))
				if ok = l >= 5 && len(b) >= l; ok {
					n = l
					if t == 0x03 {
						sub, err := bsonDecode(b[:l])
						if err != nil {
							return nil, err
						}
						val = sub
					} else {
						val = true // arrays: presence only
					}
				}
			}
		case 0x05: // binary
			if ok = len(b) >= 5; ok {
				l := int(int32(binary.LittleEndian.Uint32(b)))
				if ok = l >= 0 && len(b) >= 5+l; ok {
					val, n = append([]byte(nil), b[5:5+l]...), 5+l
				}
			}
		case 0x06, 0x0A, 0xFF, 0x7F: // undefined, null, min/max key
			val = nil
		case 0x07: // objectid
			ok = fixed(12)
		case 0x08:
			if ok = fixed(1); ok {
				val = b[0] == 1
			}
		case 0x09, 0x11: // datetime, timestamp
			if ok = fixed(8); ok {
				val = int64(binary.LittleEndian.Uint64(b))
			}
		case 0x0B: // regex: two cstrings
			i := bytes.IndexByte(b, 0)
			if ok = i >= 0; ok {
				k := bytes.IndexByte(b[i+1:], 0)
				if ok = k >= 0; ok {
					n = i + k + 2
				}
			}
		case 0x0C: // dbpointer
			if ok = len(b) >= 4; ok {
				l := int(int32(binary.LittleEndian.Uint32(b)))
				ok = fixed(4 + l + 12)
			}
		case 0x0F: // code with scope
			if ok = len(b) >= 4; ok {
				ok = fixed(int(int32(binary.LittleEndian.Uint32(b))))
			}
		case 0x10:
			if ok = fixed(4); ok {
				val = int32(binary.LittleEndian.Uint32(b))
			}
		case 0x12:
			if ok = fixed(8); ok {
				val = int64(binary.LittleEndian.Uint64(b))
			}
		case 0x13: // decimal128
			ok = fixed(16)
		default:
			return nil, errBSON
		}
		if !ok || n < 0 || n > len(b) {
			return nil, errBSON
		}
		d = append(d, bsonElem{key, val})
		b = b[n:]
	}
	return d, nil
}
