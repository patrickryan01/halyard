package mime

import (
	"io"
	"strings"

	"golang.org/x/text/encoding/htmlindex"
)

func charsetReader(label string, r io.Reader) (io.Reader, error) {
	enc, err := htmlindex.Get(strings.TrimSpace(label))
	if err != nil {
		return nil, err
	}
	return enc.NewDecoder().Reader(r), nil
}
