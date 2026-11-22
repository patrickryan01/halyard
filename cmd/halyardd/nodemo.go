//go:build !demo

package main

import "errors"

func seedDemo(string, int) error { return errors.New("this build has no demo data") }
