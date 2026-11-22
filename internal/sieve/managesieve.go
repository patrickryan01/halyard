// Package sieve is a ManageSieve (RFC 5804) client: it lists, reads, writes, checks, activates and deletes the Sieve
// filter scripts a mail server runs, and it keeps the vacation reply (RFC 5230) in the active script.
package sieve

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// Config says how to reach the server.
type Config struct {
	Host     string
	Port     int    // default 4190
	Security string // starttls (default), ssl or none
	User     string
	Password string
	// Token is an OAuth access token; when set it is used instead of the password (XOAUTH2).
	Token      string
	SkipVerify bool
	Timeout    time.Duration
}

// Client is one logged in session.
type Client struct {
	conn net.Conn
	r    *bufio.Reader
	cfg  Config
	// Caps are the server's capabilities from its last CAPABILITY answer (upper case keys).
	Caps map[string]string
}

type response struct {
	lines [][]string // each line as tokens (quoted strings and literals, atoms)
	ok    bool
	code  string
	text  string
	kind  string // OK, NO, BYE
}

func (c *Client) deadline() {
	d := c.cfg.Timeout
	if d == 0 {
		d = 30 * time.Second
	}
	_ = c.conn.SetDeadline(time.Now().Add(d))
}

// Dial connects and logs in.
func Dial(cfg Config) (*Client, error) {
	if cfg.Port == 0 {
		cfg.Port = 4190
	}
	if cfg.Security == "" {
		cfg.Security = "starttls"
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	d := net.Dialer{Timeout: 15 * time.Second}
	tc := &tls.Config{ServerName: cfg.Host, InsecureSkipVerify: cfg.SkipVerify}
	var conn net.Conn
	var err error
	if cfg.Security == "ssl" {
		conn, err = tls.DialWithDialer(&d, "tcp", addr, tc)
	} else {
		conn, err = d.Dial("tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("could not reach the filter service at %s: %w", addr, err)
	}
	c := &Client{conn: conn, r: bufio.NewReader(conn), cfg: cfg}
	c.deadline()
	if _, err := c.read(); err != nil { // the greeting carries the capabilities
		conn.Close()
		return nil, err
	}
	if cfg.Security == "starttls" {
		r, err := c.cmd("STARTTLS")
		if err != nil || !r.ok {
			conn.Close()
			return nil, errors.New("the server does not offer a secure connection for filters")
		}
		tcn := tls.Client(conn, tc)
		if err := tcn.Handshake(); err != nil {
			conn.Close()
			return nil, err
		}
		c.conn, c.r = tcn, bufio.NewReader(tcn)
	}
	if err := c.capability(); err != nil {
		c.Close()
		return nil, err
	}
	if err := c.login(); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) Close() error {
	_, _ = c.cmd("LOGOUT")
	return c.conn.Close()
}

// quote makes a ManageSieve string: a quoted string when plain, else a literal.
func quote(s string) string {
	if strings.ContainsAny(s, "\r\n\x00") || len(s) > 1000 {
		return fmt.Sprintf("{%d+}\r\n%s", len(s), s)
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// tokens reads one response line, pulling in a literal when the line ends with {n} or {n+}.
func (c *Client) readLine() ([]string, string, error) {
	var toks []string
	line, err := c.r.ReadString('\n')
	if err != nil {
		return nil, "", err
	}
	line = strings.TrimRight(line, "\r\n")
	raw := line
	for {
		i := 0
		for i < len(line) {
			switch {
			case line[i] == ' ':
				i++
			case line[i] == '"':
				j, sb := i+1, strings.Builder{}
				for j < len(line) && line[j] != '"' {
					if line[j] == '\\' && j+1 < len(line) {
						j++
					}
					sb.WriteByte(line[j])
					j++
				}
				toks = append(toks, sb.String())
				i = j + 1
			case line[i] == '{':
				j := strings.IndexByte(line[i:], '}')
				if j < 0 {
					return nil, "", errors.New("bad literal")
				}
				n, err := strconv.Atoi(strings.TrimSuffix(line[i+1:i+j], "+"))
				if err != nil {
					return nil, "", err
				}
				buf := make([]byte, n)
				if _, err := io.ReadFull(c.r, buf); err != nil {
					return nil, "", err
				}
				toks = append(toks, string(buf))
				rest, err := c.r.ReadString('\n')
				if err != nil {
					return nil, "", err
				}
				line = strings.TrimRight(rest, "\r\n")
				i = 0
			default:
				j := i
				for j < len(line) && line[j] != ' ' {
					j++
				}
				toks = append(toks, line[i:j])
				i = j
			}
		}
		break
	}
	return toks, raw, nil
}

// read collects lines up to the status line.
func (c *Client) read() (*response, error) {
	r := &response{}
	for {
		toks, raw, err := c.readLine()
		if err != nil {
			return nil, err
		}
		if len(toks) > 0 {
			switch strings.ToUpper(toks[0]) {
			case "OK", "NO", "BYE":
				r.kind = strings.ToUpper(toks[0])
				r.ok = r.kind == "OK"
				r.text = strings.TrimSpace(strings.TrimPrefix(raw, toks[0]))
				if strings.HasPrefix(r.text, "(") {
					if k := strings.Index(r.text, ")"); k > 0 {
						r.code = r.text[1:k]
						r.text = strings.TrimSpace(r.text[k+1:])
					}
				}
				r.text = strings.Trim(r.text, `"`)
				c.noteCaps(r)
				return r, nil
			}
		}
		r.lines = append(r.lines, toks)
	}
}

func (c *Client) noteCaps(r *response) {
	caps := map[string]string{}
	for _, l := range r.lines {
		if len(l) == 0 {
			continue
		}
		v := ""
		if len(l) > 1 {
			v = l[1]
		}
		caps[strings.ToUpper(l[0])] = v
	}
	if _, ok := caps["SIEVE"]; ok || len(c.Caps) == 0 {
		if len(caps) > 0 {
			c.Caps = caps
		}
	}
}

func (c *Client) cmd(format string, a ...any) (*response, error) {
	c.deadline()
	if _, err := io.WriteString(c.conn, fmt.Sprintf(format, a...)+"\r\n"); err != nil {
		return nil, err
	}
	return c.read()
}

func (c *Client) capability() error {
	r, err := c.cmd("CAPABILITY")
	if err != nil {
		return err
	}
	if !r.ok {
		return errors.New("the filter service would not say what it supports")
	}
	return nil
}

func (c *Client) login() error {
	var r *response
	var err error
	if c.cfg.Token != "" {
		ir := base64.StdEncoding.EncodeToString([]byte("user=" + c.cfg.User + "\x01auth=Bearer " + c.cfg.Token + "\x01\x01"))
		r, err = c.cmd(`AUTHENTICATE "XOAUTH2" "%s"`, ir)
	} else {
		ir := base64.StdEncoding.EncodeToString([]byte("\x00" + c.cfg.User + "\x00" + c.cfg.Password))
		r, err = c.cmd(`AUTHENTICATE "PLAIN" "%s"`, ir)
	}
	if err != nil {
		return err
	}
	if !r.ok {
		return fmt.Errorf("the filter service refused the sign in: %s", r.text)
	}
	return c.capability() // some servers change what they offer after login
}

// Script is one stored script.
type Script struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

func (c *Client) List() ([]Script, error) {
	r, err := c.cmd("LISTSCRIPTS")
	if err != nil {
		return nil, err
	}
	if !r.ok {
		return nil, fmt.Errorf("could not list scripts: %s", r.text)
	}
	var out []Script
	for _, l := range r.lines {
		if len(l) == 0 {
			continue
		}
		out = append(out, Script{Name: l[0], Active: len(l) > 1 && strings.EqualFold(l[1], "ACTIVE")})
	}
	return out, nil
}

func (c *Client) Get(name string) (string, error) {
	r, err := c.cmd("GETSCRIPT %s", quote(name))
	if err != nil {
		return "", err
	}
	if !r.ok {
		return "", fmt.Errorf("could not read the script: %s", r.text)
	}
	if len(r.lines) == 0 || len(r.lines[0]) == 0 {
		return "", nil
	}
	return r.lines[0][0], nil
}

// Check asks the server to compile a script without storing it; the error is the server's own message.
func (c *Client) Check(script string) error {
	r, err := c.cmd("CHECKSCRIPT %s", quote(script))
	if err != nil {
		return err
	}
	if !r.ok {
		return errors.New(r.text)
	}
	return nil
}

// Put stores a script (the server checks it and refuses one that does not compile).
func (c *Client) Put(name, script string) error {
	r, err := c.cmd("PUTSCRIPT %s %s", quote(name), quote(script))
	if err != nil {
		return err
	}
	if !r.ok {
		return errors.New(r.text)
	}
	return nil
}

// Activate makes the named script the one that runs; an empty name turns filtering off.
func (c *Client) Activate(name string) error {
	r, err := c.cmd("SETACTIVE %s", quote(name))
	if err != nil {
		return err
	}
	if !r.ok {
		return errors.New(r.text)
	}
	return nil
}

func (c *Client) Delete(name string) error {
	r, err := c.cmd("DELETESCRIPT %s", quote(name))
	if err != nil {
		return err
	}
	if !r.ok {
		return errors.New(r.text)
	}
	return nil
}

func (c *Client) Rename(from, to string) error {
	r, err := c.cmd("RENAMESCRIPT %s %s", quote(from), quote(to))
	if err != nil {
		return err
	}
	if !r.ok {
		return errors.New(r.text)
	}
	return nil
}
