//go:build !windows

package main

import "errors"

func openManagementBrowser(string) error { return errors.New("Windows desktop required") }
