package main

import (
	"runtime"
	"uvplatform.com/uvp-gb28181/internal/httpsconfig"
)

func runHTTPSRender(args []string) error {
	fs := newFlagSet("https-render")
	root := fs.String("root", "", "绿色安装包根目录")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path, err := resolvePath(*root)
	if err != nil {
		return err
	}
	return httpsconfig.RenderStartup(path, runtime.GOOS)
}
