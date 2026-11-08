package check

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// Step is an HTTP request made before the monitor's own request, e.g. a
// login. Values it extracts become variables, used as {{name}} in the URL,
// headers and body of later steps and of the monitor's request. Cookies set
// by any response are sent with the next requests.
type Step struct {
	Name           string            `json:"name,omitempty"`
	Method         string            `json:"method"`
	URL            string            `json:"url"`
	Headers        map[string]string `json:"headers,omitempty"`
	Body           string            `json:"body,omitempty"`
	ExpectedStatus string            `json:"expected_status,omitempty"`
	Extract        []Extract         `json:"extract,omitempty"`
}

// Extract saves a value of a step's response as a variable.
type Extract struct {
	Var string `json:"var"`
	// From is where the value comes from: "json" (Path is a JSON path, as
	// in response rules), "header" (Path is the header name) or "regex"
	// (Path is a regular expression on the body; its first group, or the
	// whole match).
	From string `json:"from"`
	Path string `json:"path"`
}

// Extract sources.
const (
	FromJSON   = "json"
	FromHeader = "header"
	FromRegex  = "regex"
)

// MaxSteps is the most steps a monitor can have; MaxExtracts the most
// variables one step can extract.
const (
	MaxSteps    = 10
	MaxExtracts = 10
)

// StepMethods are the HTTP methods a step can use.
var StepMethods = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}

var (
	varNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	varRefRe  = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)
)

// ValidateStep checks one step and normalizes it.
func ValidateStep(s *Step) error {
	s.Name = strings.TrimSpace(s.Name)
	s.URL = strings.TrimSpace(s.URL)
	s.Method = strings.ToUpper(strings.TrimSpace(s.Method))
	if s.Method == "" {
		s.Method = http.MethodGet
	}
	if !slices.Contains(StepMethods, s.Method) {
		return fmt.Errorf("unknown method %q", s.Method)
	}
	// Variables can stand for any part of the URL: check it with
	// placeholders.
	u, err := url.Parse(varRefRe.ReplaceAllString(s.URL, "x"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("url must be an http(s) URL")
	}
	if s.Headers == nil {
		s.Headers = map[string]string{}
	}
	if len(s.Extract) > MaxExtracts {
		return fmt.Errorf("at most %d values to extract", MaxExtracts)
	}
	for i := range s.Extract {
		e := &s.Extract[i]
		e.Var = strings.TrimSpace(e.Var)
		e.Path = strings.TrimSpace(e.Path)
		if !varNameRe.MatchString(e.Var) {
			return fmt.Errorf("variable name %q: use letters, digits and _", e.Var)
		}
		switch e.From {
		case FromJSON:
			if _, err := parsePath(e.Path); err != nil {
				return fmt.Errorf("{{%s}}: path %q: %w", e.Var, e.Path, err)
			}
		case FromHeader:
			if e.Path == "" {
				return fmt.Errorf("{{%s}}: header name is required", e.Var)
			}
		case FromRegex:
			if _, err := regexp.Compile(e.Path); err != nil {
				return fmt.Errorf("{{%s}}: invalid regular expression", e.Var)
			}
		default:
			return fmt.Errorf("{{%s}}: from must be json, header or regex", e.Var)
		}
	}
	if s.Extract == nil {
		s.Extract = []Extract{}
	}
	return nil
}

// expand replaces {{name}} with the value of a variable. Unknown names are
// left as they are, so the error shows what is missing.
func expand(s string, vars map[string]string) string {
	if len(vars) == 0 || !strings.Contains(s, "{{") {
		return s
	}
	return varRefRe.ReplaceAllStringFunc(s, func(m string) string {
		name := varRefRe.FindStringSubmatch(m)[1]
		if v, ok := vars[name]; ok {
			return v
		}
		return m
	})
}

func expandHeaders(h map[string]string, vars map[string]string) map[string]string {
	if len(h) == 0 || len(vars) == 0 {
		return h
	}
	out := make(map[string]string, len(h))
	for k, v := range h {
		out[k] = expand(v, vars)
	}
	return out
}

// stepLabel names a step in messages: "step 2 (login)".
func stepLabel(i int, s Step) string {
	if s.Name != "" {
		return fmt.Sprintf("step %d (%s)", i+1, s.Name)
	}
	return fmt.Sprintf("step %d", i+1)
}

// runSteps makes the requests before the monitor's own one. It returns the
// variables they extracted, how many steps ran and, when one failed, the
// failed result.
func runSteps(ctx context.Context, j Job, client *http.Client, pt *phaseTracer) (map[string]string, int, *Result) {
	vars := map[string]string{}
	for i, s := range j.Steps {
		label := stepLabel(i, s)
		fail := func(msg string, code int) (map[string]string, int, *Result) {
			return vars, i + 1, &Result{StatusCode: code, Message: label + ": " + msg, Timing: pt.timing()}
		}
		var body io.Reader
		if s.Body != "" {
			body = strings.NewReader(expand(s.Body, vars))
		}
		req, err := http.NewRequestWithContext(ctx, s.Method, expand(s.URL, vars), body)
		if err != nil {
			return fail(err.Error(), 0)
		}
		req.Header.Set("User-Agent", userAgent)
		for k, v := range expandHeaders(s.Headers, vars) {
			req.Header.Set(k, v)
		}
		c := *client
		c.CheckRedirect = dropHeadersOnRedirect(s.Headers)
		resp, err := c.Do(req)
		if err != nil {
			return fail(cleanErr(err), 0)
		}
		content, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		if !StatusMatches(s.ExpectedStatus, resp.StatusCode) {
			return fail(fmt.Sprintf("unexpected status %d", resp.StatusCode), resp.StatusCode)
		}
		var doc any
		parsed := false
		for _, e := range s.Extract {
			var v string
			switch e.From {
			case FromHeader:
				v = resp.Header.Get(e.Path)
				if v == "" {
					return fail(fmt.Sprintf("{{%s}}: no %s header", e.Var, e.Path), resp.StatusCode)
				}
			case FromRegex:
				re, err := regexp.Compile(e.Path)
				if err != nil {
					return fail(fmt.Sprintf("{{%s}}: invalid regular expression", e.Var), resp.StatusCode)
				}
				m := re.FindSubmatch(content)
				if m == nil {
					return fail(fmt.Sprintf("{{%s}}: /%s/ does not match", e.Var, e.Path), resp.StatusCode)
				}
				v = string(m[0])
				if len(m) > 1 {
					v = string(m[1])
				}
			default: // json
				if !parsed {
					dec := json.NewDecoder(bytes.NewReader(content))
					dec.UseNumber()
					if dec.Decode(&doc) != nil {
						return fail(fmt.Sprintf("{{%s}}: response is not valid JSON", e.Var), resp.StatusCode)
					}
					parsed = true
				}
				path, err := parsePath(e.Path)
				if err != nil {
					return fail(fmt.Sprintf("{{%s}}: %v", e.Var, err), resp.StatusCode)
				}
				x, found := lookup(doc, path)
				if !found {
					return fail(fmt.Sprintf("{{%s}}: %s not found", e.Var, e.Path), resp.StatusCode)
				}
				v = text(x)
			}
			vars[e.Var] = v
		}
	}
	return vars, len(j.Steps), nil
}

// newCookieJar keeps the cookies of a multi-step check (a login session)
// for its next requests.
func newCookieJar() http.CookieJar {
	jar, _ := cookiejar.New(nil)
	return jar
}
