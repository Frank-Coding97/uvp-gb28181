// standalone-config-probe is an acceptance tool, not the product launcher.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

func main() {
	paths, err := standalone.ResolveStartupPaths(nil, os.Getenv)
	if err == nil && !paths.Explicit {
		err = fmt.Errorf("explicit instance paths are required")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var result any
	if len(os.Args) == 2 && os.Args[1] == "--acl-check" {
		result, err = checkAccountACL(paths)
	} else if len(os.Args) == 1 {
		err = prepareProbePaths(paths)
		if err == nil {
			result, err = standalone.InitializeConfig(paths)
		}
	} else {
		err = fmt.Errorf("unknown probe arguments")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err = json.NewEncoder(os.Stdout).Encode(result); err != nil {
		os.Exit(1)
	}
}

func prepareProbePaths(p standalone.Paths) error {
	for _, dir := range []string{p.InstallDir, p.ConfigDir, p.ResourceDir, p.WebDir, p.DataDir, p.RecordingsDir} {
		if err := os.MkdirAll(filepath.Clean(dir), 0755); err != nil {
			return err
		}
	}
	return nil
}
