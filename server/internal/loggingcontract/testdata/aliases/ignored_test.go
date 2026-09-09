package aliases

import "go.uber.org/zap"

func TestOnlySource() {
	zap.NewNop().Info("test source is excluded")
}
