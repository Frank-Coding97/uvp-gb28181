package netif

import (
	"os"
	"strings"
)

// containerProbe 抽象容器探测所需的文件系统/环境读取，便于单测注入。
type containerProbe struct {
	stat     func(string) bool
	readFile func(string) string
	getenv   func(string) string
}

func systemContainerProbe() containerProbe {
	return containerProbe{
		stat: func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		},
		readFile: func(path string) string {
			raw, err := os.ReadFile(path)
			if err != nil {
				return ""
			}
			return string(raw)
		},
		getenv: os.Getenv,
	}
}

// containerMarkerFiles 是各运行时留下的"我在容器里"标记文件。
var containerMarkerFiles = []string{
	"/.dockerenv",        // Docker
	"/run/.containerenv", // Podman
}

// containerCgroupMarkers 是 cgroup 内容里的容器字样（cgroup v1 / 部分 v2 主机）。
var containerCgroupMarkers = []string{"docker", "kubepods", "containerd", "libpod", "lxc", "podman"}

// containerMountMarkers 是 mountinfo 里的容器运行时痕迹（cgroup v2 主机上
// /proc/1/cgroup 可能只有 "0::/"，只能退到这一层）。
// 容器自己的 root 是 overlay，mountinfo 里会带上宿主机侧的 lowerdir 路径，
// 例如 `lowerdir=/var/lib/docker/overlay2/l/XXXX`。
var containerMountMarkers = []string{
	"/var/lib/docker/",
	"docker/overlay",
	"containers/overlay",
	"/kubepods",
}

// InContainer 报告当前进程是否运行在容器里（Docker / Podman / Kubernetes / LXC）。
//
// 用途：容器内的网卡列表只有 `eth0` + `lo`，"虚拟网卡"判定根本不会触发，
// 向导会把**容器内地址**当成推荐地址推荐出去。而 GB28181 要求设备主动把 RTP 推回平台，
// 容器地址在局域网里路由不到 ⇒ 只能提示用户改填宿主机地址。见 setup.ContainerAddressWarning。
func InContainer() bool {
	return detectInContainer(systemContainerProbe())
}

func detectInContainer(probe containerProbe) bool {
	if probe.getenv("KUBERNETES_SERVICE_HOST") != "" {
		return true
	}
	for _, path := range containerMarkerFiles {
		if probe.stat(path) {
			return true
		}
	}

	cgroup := strings.ToLower(probe.readFile("/proc/1/cgroup"))
	for _, marker := range containerCgroupMarkers {
		if strings.Contains(cgroup, marker) {
			return true
		}
	}

	mountinfo := strings.ToLower(probe.readFile("/proc/self/mountinfo"))
	for _, marker := range containerMountMarkers {
		if strings.Contains(mountinfo, marker) {
			return true
		}
	}
	return false
}
