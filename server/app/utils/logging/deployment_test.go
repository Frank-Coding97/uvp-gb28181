package logging

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"go.uber.org/zap/zapcore"
)

func TestLoggingDeploymentTemplate(t *testing.T) {
	data, err := os.ReadFile("../../../config/config.example.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, debug := range []bool{true, false} {
		v := viper.New()
		v.SetConfigType("yaml")
		if err := v.ReadConfig(strings.NewReader(string(data))); err != nil {
			t.Fatal(err)
		}
		v.Set("server.appdebug", debug)
		c, err := ParseConfig(v, "/app")
		if err != nil {
			t.Fatal(err)
		}
		if len(c.Notices) != 0 {
			t.Fatalf("new template must not configure retired logging fields: %v", c.Notices)
		}
		if !reflect.DeepEqual(c.Outputs, []string{"file", "stdout"}) || c.FileFormat != "console" || c.StdoutFormat != "console" {
			t.Fatalf("development outputs changed with appdebug=%v: %+v", debug, c)
		}
		if c.Modules["access"] != zapcore.InfoLevel || c.Modules["scheduler"] != zapcore.InfoLevel || c.Routes || c.MaxSizeMB != 5 || c.MaxBackups != 7 || c.MaxAgeDays != 15 {
			t.Fatalf("invalid deployment defaults: %+v", c)
		}
		if v.GetInt("scheduler.job_results_buffer_size") != 1000 {
			t.Fatal("logging migration changed the result queue capacity")
		}
	}
}

func TestLoggingDeploymentBasePaths(t *testing.T) {
	for _, base := range []string{"/app", "/opt/uvp-gb28181/releases/example/backend"} {
		for _, legacy := range []string{"./resource/logs/uvp-gb28181.log", "/resource/logs/uvp-gb28181.log"} {
			input := values{"logs.zaplogname": legacy, "logs.console": false}
			c, err := ParseConfig(input, base)
			if err != nil {
				t.Fatal(err)
			}
			if c.FilePath != filepath.Join(base, "resource/logs/uvp-gb28181.log") || !reflect.DeepEqual(c.Outputs, []string{"file"}) {
				t.Fatalf("legacy path/output mismatch: %+v", c)
			}
			input["logs.filepath"] = "/var/log/uvp/service.log"
			input["logs.outputs"] = []string{"stdout"}
			c, err = ParseConfig(input, base)
			if err != nil || c.FilePath != "/var/log/uvp/service.log" || !reflect.DeepEqual(c.Outputs, []string{"stdout"}) {
				t.Fatalf("explicit fields must win: %+v, %v", c, err)
			}
		}
	}
}
