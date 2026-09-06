package ymlconfig

import (
	"context"
	"os"
	"sync"
	"time"

	"uvplatform.cn/uvp-gb28181/app/global/app"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"

	"go.uber.org/zap"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

func CreateYamlFactory(path string, fileName ...string) app.YmlConfigInterf {
	config, err := LoadYamlFactory(path, fileName...)
	if err != nil {
		logging.ReportStartupFailure(nil, app.ZapLog, "config", err)
		if app.LogRuntime != nil {
			_ = app.LogRuntime.Close()
		}
		os.Exit(1)
	}
	return config
}

func LoadYamlFactory(path string, fileName ...string) (app.YmlConfigInterf, error) {

	yamlConfig := viper.New()
	// 配置文件所在目录
	yamlConfig.AddConfigPath(path)
	// 需要读取的文件名,默认为：config
	if len(fileName) == 0 {
		yamlConfig.SetConfigName("config")
	} else {
		yamlConfig.SetConfigName(fileName[0])
	}
	//设置配置文件类型(后缀)为 yml
	yamlConfig.SetConfigType("yml")

	// 读取配置文件
	if err := yamlConfig.ReadInConfig(); err != nil {
		return nil, err
	}

	return &ymlConfig{
		viper:          yamlConfig,
		mu:             new(sync.RWMutex),
		callbackState:  newConfigCallbackState(),
		lastChangeTime: time.Now(),
	}, nil
}

type ymlConfig struct {
	viper          *viper.Viper
	mu             *sync.RWMutex
	callbackState  *configCallbackState
	callbackOrder  sync.Mutex
	lastChangeTime time.Time
}

type configCallbackState struct {
	mu        sync.Mutex
	stopping  bool
	inFlight  int
	callbacks chan struct{}
}

func newConfigCallbackState() *configCallbackState {
	return &configCallbackState{callbacks: make(chan struct{})}
}

func (s *configCallbackState) admit() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return false
	}
	s.inFlight++
	return true
}

func (s *configCallbackState) finish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inFlight--
	if s.stopping && s.inFlight == 0 {
		close(s.callbacks)
	}
}

func (s *configCallbackState) stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	s.stopping = true
	if s.inFlight == 0 {
		s.mu.Unlock()
		return nil
	}
	done := s.callbacks
	s.mu.Unlock()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ConfigFileChangeListen 监听文件变化
func (y *ymlConfig) ConfigFileChangeListen(fns ...func()) {

	y.viper.OnConfigChange(func(changeEvent fsnotify.Event) {
		if !y.callbackState.admit() {
			return
		}
		defer y.callbackState.finish()

		y.callbackOrder.Lock()
		defer y.callbackOrder.Unlock()
		if time.Since(y.lastChangeTime).Seconds() >= 1 {
			if changeEvent.Op.String() == "WRITE" {

				// 重新读取配置文件（使用写锁保护）
				y.mu.Lock()
				if err := y.viper.ReadInConfig(); err != nil {
					logging.ReportStartupFailure(nil, app.ZapLog, "config", err)
				} else {
					if app.ZapLog != nil {
						app.ZapLog.Named("config").Info("configuration reloaded", zap.String("event", "config.reloaded"))
					}
				}
				y.mu.Unlock()
				// 执行自定义回调函数
				for _, f := range fns {
					f()
				}
				y.lastChangeTime = time.Now()
			}
		}
	})
	y.viper.WatchConfig()
}

// StopContext stops admitting configuration callbacks and waits for callbacks
// already admitted to finish. It does not stop Viper's configuration reader.
func (y *ymlConfig) StopContext(ctx context.Context) error {
	return y.callbackState.stop(ctx)
}

func (y *ymlConfig) Get(keyName string) interface{} {
	y.mu.RLock()
	defer y.mu.RUnlock()
	value := y.viper.Get(keyName)
	return value
}

func (y *ymlConfig) GetString(keyName string) string {
	y.mu.RLock()
	defer y.mu.RUnlock()
	value := y.viper.GetString(keyName)
	return value
}

func (y *ymlConfig) GetBool(keyName string) bool {
	y.mu.RLock()
	defer y.mu.RUnlock()
	value := y.viper.GetBool(keyName)
	return value
}

func (y *ymlConfig) GetInt(keyName string) int {
	y.mu.RLock()
	defer y.mu.RUnlock()
	value := y.viper.GetInt(keyName)
	return value

}

func (y *ymlConfig) GetInt32(keyName string) int32 {
	y.mu.RLock()
	defer y.mu.RUnlock()
	value := y.viper.GetInt32(keyName)
	return value
}

func (y *ymlConfig) GetInt64(keyName string) int64 {
	y.mu.RLock()
	defer y.mu.RUnlock()
	value := y.viper.GetInt64(keyName)
	return value
}

func (y *ymlConfig) GetFloat64(keyName string) float64 {
	y.mu.RLock()
	defer y.mu.RUnlock()
	value := y.viper.GetFloat64(keyName)
	return value

}

func (y *ymlConfig) GetDuration(keyName string) time.Duration {
	y.mu.RLock()
	defer y.mu.RUnlock()
	value := y.viper.GetDuration(keyName)
	return value

}

func (y *ymlConfig) GetStringSlice(keyName string) []string {
	y.mu.RLock()
	defer y.mu.RUnlock()
	value := y.viper.GetStringSlice(keyName)
	return value
}

func (y *ymlConfig) GetUintSlice(keyName string) []uint {
	y.mu.RLock()
	defer y.mu.RUnlock()

	// 首先尝试直接获取uint切片
	if value := y.viper.Get(keyName); value != nil {
		if uintSlice, ok := value.([]uint); ok {
			return uintSlice
		}
	}

	// 如果直接获取失败，尝试从int切片转换
	intSlice := y.viper.GetIntSlice(keyName)
	if len(intSlice) == 0 {
		return []uint{}
	}

	// 将int切片转换为uint切片
	uintSlice := make([]uint, len(intSlice))
	for i, v := range intSlice {
		if v < 0 {
			// 如果值为负数，设置为0
			uintSlice[i] = 0
		} else {
			uintSlice[i] = uint(v)
		}
	}

	return uintSlice
}

// Set 设置配置值
func (y *ymlConfig) Set(keyName string, value interface{}) {
	y.mu.Lock()
	defer y.mu.Unlock()
	y.viper.Set(keyName, value)
}

// SaveConfig 保存配置到文件
func (y *ymlConfig) SaveConfig() error {
	y.mu.Lock()
	defer y.mu.Unlock()
	// 保存当前的配置值
	currentSettings := y.viper.AllSettings()

	// 重新读取配置文件，确保所有原始配置项都被加载
	if err := y.viper.ReadInConfig(); err != nil {
		return err
	}

	// 将修改后的配置项重新设置到 viper 中
	for key, value := range currentSettings {
		y.viper.Set(key, value)
	}

	// 写入配置
	err := y.viper.WriteConfig()
	if err != nil {
		return err
	}
	return nil
}
