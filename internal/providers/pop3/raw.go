package pop3

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

// Raw downloads the message again and returns it as it is.
func (p *Provider) Raw(ctx context.Context, msgID string) ([]byte, error) {
	c, err := p.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer c.close()
	ul, err := c.multi("UIDL")
	if err != nil {
		return nil, err
	}
	for _, l := range ul {
		f := strings.Fields(l)
		if len(f) == 2 && f[1] == msgID {
			n, _ := strconv.Atoi(f[0])
			return c.retr(n)
		}
	}
	return nil, errors.New("the server no longer has this message")
}
