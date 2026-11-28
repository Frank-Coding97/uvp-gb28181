package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 节点名/别名是**纯展示字段**（不写进 ZLM、不参与调度判据），但它的落库列是
// `meta_node.name varchar(64)`。这组用例锁住两件事：
//  1. 长度按**字符**算（与 MySQL utf8mb4 的 varchar(64) 同口径），中文别名不该被字节数误拒；
//  2. 留空仍然走 `host:port` 兜底，新建向导不填别名时行为不变。
func TestValidateNodeName_CountsRunesNotBytes(t *testing.T) {
	require.NoError(t, validateNodeName(strings.Repeat("电", maxNodeNameLen)))
	require.NoError(t, validateNodeName(""))

	err := validateNodeName(strings.Repeat("电", maxNodeNameLen+1))
	require.Error(t, err)
	require.Contains(t, err.Error(), "最长")
}

func TestBuildCreateCandidate_NodeNameFallsBackToHostPort(t *testing.T) {
	base := CreateNodeReq{Host: "10.0.0.7", APIPort: 18080, APISecret: "secret"}

	withoutAlias, err := buildCreateCandidate(base)
	require.NoError(t, err)
	require.Equal(t, "10.0.0.7:18080", withoutAlias.Name)

	withAlias := base
	withAlias.Name = "  一楼弱电间  "
	named, err := buildCreateCandidate(withAlias)
	require.NoError(t, err)
	require.Equal(t, "一楼弱电间", named.Name)
}

func TestBuildCreateCandidate_RejectsOverlongAlias(t *testing.T) {
	req := CreateNodeReq{Host: "10.0.0.7", APIPort: 18080, APISecret: "secret", Name: strings.Repeat("电", maxNodeNameLen+1)}

	_, err := buildCreateCandidate(req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "最长")
}
