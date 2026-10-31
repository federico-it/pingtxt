package check

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Assertion is a rule on the JSON body of an HTTP response, e.g.
// {"path": "checks.redis.status", "op": "eq", "value": "ok"}.
type Assertion struct {
	Path  string `json:"path"`
	Op    string `json:"op"`
	Value string `json:"value,omitempty"`
}

// Assertion operators.
const (
	OpEquals      = "eq"
	OpNotEquals   = "ne"
	OpContains    = "contains"
	OpNotContains = "not_contains"
	OpExists      = "exists"
	OpNotExists   = "not_exists"
	OpGreater     = "gt"
	OpGreaterEq   = "gte"
	OpLess        = "lt"
	OpLessEq      = "lte"
	OpMatches     = "matches"
)

// AssertionOps lists the valid operators.
var AssertionOps = []string{OpEquals, OpNotEquals, OpContains, OpNotContains, OpExists, OpNotExists,
	OpGreater, OpGreaterEq, OpLess, OpLessEq, OpMatches}

// MaxAssertions is the most rules a monitor can have.
const MaxAssertions = 20

var opSymbols = map[string]string{
	OpEquals: "==", OpNotEquals: "!=", OpGreater: ">", OpGreaterEq: ">=", OpLess: "<", OpLessEq: "<=",
}

// ValidateAssertion checks one rule and normalizes it.
func ValidateAssertion(a *Assertion) error {
	a.Path = strings.TrimSpace(a.Path)
	a.Op = strings.TrimSpace(a.Op)
	if _, err := parsePath(a.Path); err != nil {
		return fmt.Errorf("path %q: %w", a.Path, err)
	}
	switch a.Op {
	case OpExists, OpNotExists:
		a.Value = ""
	case OpGreater, OpGreaterEq, OpLess, OpLessEq:
		a.Value = strings.TrimSpace(a.Value)
		if _, err := strconv.ParseFloat(a.Value, 64); err != nil {
			return fmt.Errorf("path %q: %s needs a number", a.Path, opSymbols[a.Op])
		}
	case OpMatches:
		if _, err := regexp.Compile(a.Value); err != nil {
			return fmt.Errorf("path %q: invalid regular expression", a.Path)
		}
	case OpEquals, OpNotEquals, OpContains, OpNotContains:
	default:
		return fmt.Errorf("path %q: unknown operator %q", a.Path, a.Op)
	}
	return nil
}

// CheckAssertions evaluates the rules on a JSON body. It returns how many
// rules passed and, when one fails, why.
func CheckAssertions(body []byte, rules []Assertion) (int, string) {
	if len(rules) == 0 {
		return 0, ""
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return 0, "response is not valid JSON"
	}
	for i, a := range rules {
		if msg := evalAssertion(doc, a); msg != "" {
			return i, msg
		}
	}
	return len(rules), ""
}

func evalAssertion(doc any, a Assertion) string {
	name := a.Path
	if name == "" {
		name = "$"
	}
	path, err := parsePath(a.Path)
	if err != nil {
		return fmt.Sprintf("%s: %v", name, err)
	}
	v, found := lookup(doc, path)
	switch a.Op {
	case OpExists:
		if !found {
			return name + ": not found"
		}
		return ""
	case OpNotExists:
		if found {
			return fmt.Sprintf("%s: should not exist, got %s", name, show(v))
		}
		return ""
	}
	if !found {
		return name + ": not found"
	}
	switch a.Op {
	case OpEquals:
		if !equals(v, a.Value) {
			return fmt.Sprintf("%s: expected %s, got %s", name, showExpected(v, a.Value), show(v))
		}
	case OpNotEquals:
		if equals(v, a.Value) {
			return fmt.Sprintf("%s: expected anything but %s", name, show(v))
		}
	case OpContains, OpNotContains:
		has, ok := contains(v, a.Value)
		if !ok {
			return fmt.Sprintf("%s: cannot search in %s", name, show(v))
		}
		if has != (a.Op == OpContains) {
			if has {
				return fmt.Sprintf("%s: should not contain %q, got %s", name, a.Value, show(v))
			}
			return fmt.Sprintf("%s: should contain %q, got %s", name, a.Value, show(v))
		}
	case OpGreater, OpGreaterEq, OpLess, OpLessEq:
		want, err := strconv.ParseFloat(strings.TrimSpace(a.Value), 64)
		if err != nil {
			return fmt.Sprintf("%s: %q is not a number", name, a.Value)
		}
		got, ok := number(v)
		if !ok {
			return fmt.Sprintf("%s: expected a number, got %s", name, show(v))
		}
		pass := map[string]bool{OpGreater: got > want, OpGreaterEq: got >= want, OpLess: got < want, OpLessEq: got <= want}[a.Op]
		if !pass {
			return fmt.Sprintf("%s: expected %s %s, got %s", name, opSymbols[a.Op], a.Value, show(v))
		}
	case OpMatches:
		re, err := regexp.Compile(a.Value)
		if err != nil {
			return fmt.Sprintf("%s: invalid regular expression", name)
		}
		if !re.MatchString(text(v)) {
			return fmt.Sprintf("%s: %s does not match /%s/", name, show(v), a.Value)
		}
	default:
		return fmt.Sprintf("%s: unknown operator %q", name, a.Op)
	}
	return ""
}

// parsePath splits "checks.redis.status", "items[0].id", "items.0.id" or
// `headers["content-type"]` into keys. A leading "$" or "$." is ignored and
// an empty path means the whole document.
func parsePath(p string) ([]string, error) {
	p = strings.TrimPrefix(strings.TrimSpace(p), "$")
	out := []string{}
	i := 0
	if strings.HasPrefix(p, ".") {
		i = 1
	}
	key := func() error {
		j := i
		for j < len(p) && p[j] != '.' && p[j] != '[' {
			j++
		}
		if j == i {
			return errors.New("empty key")
		}
		out = append(out, strings.TrimSpace(p[i:j]))
		i = j
		return nil
	}
	if i < len(p) && p[i] != '[' {
		if err := key(); err != nil {
			return nil, err
		}
	}
	for i < len(p) {
		switch p[i] {
		case '.':
			i++
			if err := key(); err != nil {
				return nil, err
			}
		case '[':
			rest := p[i+1:]
			if rest != "" && (rest[0] == '"' || rest[0] == '\'') {
				end := strings.IndexByte(rest[1:], rest[0])
				if end < 0 {
					return nil, errors.New("unterminated quote")
				}
				if !strings.HasPrefix(rest[end+2:], "]") {
					return nil, errors.New("missing ]")
				}
				out = append(out, rest[1:end+1])
				i += end + 4
				continue
			}
			end := strings.IndexByte(rest, ']')
			if end < 0 {
				return nil, errors.New("missing ]")
			}
			idx := strings.TrimSpace(rest[:end])
			if _, err := strconv.Atoi(idx); err != nil {
				return nil, fmt.Errorf("invalid index [%s]", idx)
			}
			out = append(out, idx)
			i += end + 2
		default:
			return nil, errors.New("expected . or [ after ]")
		}
	}
	return out, nil
}

func lookup(v any, path []string) (any, bool) {
	for _, k := range path {
		switch c := v.(type) {
		case map[string]any:
			x, ok := c[k]
			if !ok {
				return nil, false
			}
			v = x
		case []any:
			n, err := strconv.Atoi(k)
			if err != nil {
				return nil, false
			}
			if n < 0 {
				n += len(c)
			}
			if n < 0 || n >= len(c) {
				return nil, false
			}
			v = c[n]
		default:
			return nil, false
		}
	}
	return v, true
}

func number(v any) (float64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	f, err := n.Float64()
	return f, err == nil && !math.IsInf(f, 0)
}

// equals compares a JSON value with the text of a rule: strings as they are,
// numbers numerically, true/false/null by name and objects or arrays as
// JSON.
func equals(v any, want string) bool {
	switch x := v.(type) {
	case string:
		return x == want
	case json.Number:
		w, err := strconv.ParseFloat(strings.TrimSpace(want), 64)
		f, _ := number(x)
		return err == nil && f == w
	case bool:
		return strings.EqualFold(strings.TrimSpace(want), strconv.FormatBool(x))
	case nil:
		return strings.EqualFold(strings.TrimSpace(want), "null")
	default:
		var w any
		dec := json.NewDecoder(strings.NewReader(want))
		dec.UseNumber()
		if dec.Decode(&w) != nil {
			return false
		}
		a, _ := json.Marshal(v)
		b, _ := json.Marshal(w)
		return bytes.Equal(a, b)
	}
}

// contains reports whether a string has a substring, an array has an
// element equal to want or an object has the key want.
func contains(v any, want string) (has, ok bool) {
	switch x := v.(type) {
	case string:
		return strings.Contains(x, want), true
	case []any:
		for _, e := range x {
			if equals(e, want) {
				return true, true
			}
		}
		return false, true
	case map[string]any:
		_, has := x[want]
		return has, true
	}
	return false, false
}

func text(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func show(v any) string {
	if s, ok := v.(string); ok {
		return strconv.Quote(truncate(s, 60))
	}
	b, _ := json.Marshal(v)
	return truncate(string(b), 60)
}

func showExpected(v any, want string) string {
	if _, ok := v.(string); ok {
		return strconv.Quote(want)
	}
	return want
}
