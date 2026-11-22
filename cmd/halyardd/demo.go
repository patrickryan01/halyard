//go:build demo

package main

import (
	"github.com/patrickryan01/halyard/internal/devseed"
	"github.com/patrickryan01/halyard/internal/store"
)

// seedDemo fills a data directory with made up mail for design renders.
func seedDemo(dir string, accounts int) error {
	st, err := store.Open(dir + "/mail.db")
	if err != nil {
		return err
	}
	defer st.Close()
	return devseed.Seed(st, accounts)
}
