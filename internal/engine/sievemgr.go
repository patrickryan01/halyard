package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/patrickryan01/halyard/internal/sieve"
)

type sieveProvider interface {
	Sieve(ctx context.Context) (*sieve.Client, error)
}

// SieveClient opens the filter service of an account, for the account kinds whose server has one.
func (e *Engine) SieveClient(ctx context.Context, accountID string) (*sieve.Client, error) {
	p, acct, err := e.provider(accountID)
	if err != nil {
		return nil, err
	}
	defer p.Close()
	sp, ok := p.(sieveProvider)
	if !ok {
		return nil, fmt.Errorf("%s accounts have no server side filters", acct.Kind)
	}
	return sp.Sieve(ctx)
}

// SieveScripts lists the scripts on the server.
func (e *Engine) SieveScripts(ctx context.Context, accountID string) ([]sieve.Script, error) {
	c, err := e.SieveClient(ctx, accountID)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.List()
}

func (e *Engine) SieveScript(ctx context.Context, accountID, name string) (string, error) {
	c, err := e.SieveClient(ctx, accountID)
	if err != nil {
		return "", err
	}
	defer c.Close()
	return c.Get(name)
}

// SievePut stores a script after the server has compiled it; the server's own error comes back when it does not.
func (e *Engine) SievePut(ctx context.Context, accountID, name, script string, activate bool) error {
	if name == "" {
		return errors.New("a script needs a name")
	}
	c, err := e.SieveClient(ctx, accountID)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Put(name, script); err != nil {
		return err
	}
	if activate {
		return c.Activate(name)
	}
	return nil
}

func (e *Engine) SieveCheck(ctx context.Context, accountID, script string) error {
	c, err := e.SieveClient(ctx, accountID)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Check(script)
}

func (e *Engine) SieveActivate(ctx context.Context, accountID, name string) error {
	c, err := e.SieveClient(ctx, accountID)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Activate(name)
}

func (e *Engine) SieveDelete(ctx context.Context, accountID, name string) error {
	c, err := e.SieveClient(ctx, accountID)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Delete(name)
}
