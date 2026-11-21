//go:build !windows

package main

import "os"

func defaultLocalUID() uint { return uint(os.Getuid()) }
func defaultLocalGID() uint { return uint(os.Getgid()) }
