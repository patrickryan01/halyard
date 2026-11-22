// Package importer reads mail out of the formats other clients keep it in: mbox files, and Thunderbird profiles (which
// are folders of mbox files).
package importer

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Mbox splits an mbox stream into raw messages. Both the "From " separator line and ">From " quoting (mboxrd and mboxo)
// are handled.
func Mbox(r io.Reader, each func(raw []byte) error) error {
	br := bufio.NewReaderSize(r, 1<<20)
	var cur bytes.Buffer
	started := false
	flush := func() error {
		if !started || cur.Len() == 0 {
			return nil
		}
		raw := bytes.TrimRight(cur.Bytes(), "\r\n")
		raw = append(append([]byte{}, raw...), '\r', '\n')
		cur.Reset()
		return each(raw)
	}
	prevBlank := true
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			if prevBlank && bytes.HasPrefix(line, []byte("From ")) && !started || prevBlank && bytes.HasPrefix(line, []byte("From ")) && started {
				if e := flush(); e != nil {
					return e
				}
				started = true
				prevBlank = false
				if err == io.EOF {
					break
				}
				continue
			}
			if started {
				if bytes.HasPrefix(line, []byte(">From ")) {
					line = line[1:]
				}
				cur.Write(bytes.ReplaceAll(bytes.TrimRight(line, "\r\n"), []byte("\x00"), nil))
				cur.WriteString("\r\n")
			}
			prevBlank = len(bytes.TrimRight(line, "\r\n")) == 0
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}
	return flush()
}

// Folder is one mailbox found in a profile: its path of names and the mbox file that holds it.
type Folder struct {
	Name string // "Inbox", "Projects/2024"
	Path string
}

// Thunderbird lists the mbox folders in a Thunderbird profile directory: Mail/<account>/<folder> files with their
// <folder>.sbd subfolder directories, and ImapMail/<server>/... the same way. Index files (.msf), account metadata
// and the search folders are skipped.
func Thunderbird(profile string) ([]Folder, error) {
	var out []Folder
	for _, root := range []string{"Mail", "ImapMail"} {
		accts, err := os.ReadDir(filepath.Join(profile, root))
		if err != nil {
			continue
		}
		for _, a := range accts {
			if !a.IsDir() {
				continue
			}
			prefix := ""
			if a.Name() != "Local Folders" {
				prefix = a.Name() + "/"
			}
			walkTB(filepath.Join(profile, root, a.Name()), prefix, &out)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if len(out) == 0 {
		return nil, os.ErrNotExist
	}
	return out, nil
}

func walkTB(dir, prefix string, out *[]Folder) {
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		n := e.Name()
		switch {
		case e.IsDir() && strings.HasSuffix(n, ".sbd"):
			walkTB(filepath.Join(dir, n), prefix+strings.TrimSuffix(n, ".sbd")+"/", out)
		case e.IsDir(), strings.HasSuffix(n, ".msf"), strings.HasSuffix(n, ".dat"), strings.HasSuffix(n, ".json"), strings.HasSuffix(n, ".html"), n == "msgFilterRules.dat", strings.HasPrefix(n, "."):
		default:
			if fi, err := e.Info(); err == nil && fi.Size() > 0 {
				*out = append(*out, Folder{Name: prefix + n, Path: filepath.Join(dir, n)})
			}
		}
	}
}
