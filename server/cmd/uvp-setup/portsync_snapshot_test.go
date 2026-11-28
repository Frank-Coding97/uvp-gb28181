package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncZLMIniSnapshotTCP(t *testing.T) {
	const factory = "%s -i %s -y -f mjpeg -frames:v 1 -an %s"
	const tcp = "%s -rtsp_transport tcp -i %s -y -f mjpeg -frames:v 1 -an %s"
	for _, tc := range []struct{ name, current, want string }{
		{"factory", factory, tcp},
		{"already TCP", tcp, tcp},
		{"custom", "%s -i %s -vf scale=640:-1 -frames:v 1 %s", "%s -i %s -vf scale=640:-1 -frames:v 1 %s"},
	} {
		for _, eol := range []string{"\n", "\r\n"} {
			t.Run(tc.name+"/"+strings.ReplaceAll(eol, "\r", "CR"), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "zlm.ini")
				before := strings.Join([]string{"[ffmpeg]", "bin=./bin/ffmpeg", "snap=" + tc.current, "cmd=custom-push", "[other]", "snap=" + factory, ""}, eol)
				if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
					t.Fatal(err)
				}
				want := strings.Replace(before, "snap="+tc.current+eol, "snap="+tc.want+eol, 1)
				for i := 0; i < 2; i++ {
					if err := syncZLMIni(path, zlmPorts{}, "", ""); err != nil {
						t.Fatal(err)
					}
					after, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					if string(after) != want {
						t.Fatalf("unexpected config after sync %d:\n%s\nwant:\n%s", i, after, want)
					}
				}
			})
		}
	}
}
