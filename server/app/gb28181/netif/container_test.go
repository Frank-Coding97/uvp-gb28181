package netif

import "testing"

func probeOf(files map[string]string, env map[string]string) containerProbe {
	return containerProbe{
		stat: func(path string) bool {
			_, ok := files[path]
			return ok
		},
		readFile: func(path string) string {
			return files[path]
		},
		getenv: func(key string) string {
			return env[key]
		},
	}
}

func TestDetectInContainer(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		env   map[string]string
		want  bool
	}{
		{
			name:  "docker 有 /.dockerenv",
			files: map[string]string{"/.dockerenv": ""},
			want:  true,
		},
		{
			name:  "podman 有 /run/.containerenv",
			files: map[string]string{"/run/.containerenv": ""},
			want:  true,
		},
		{
			name: "cgroup v1 带 docker 字样",
			files: map[string]string{
				"/proc/1/cgroup": "12:pids:/docker/3f2a1b\n11:cpu:/docker/3f2a1b\n",
			},
			want: true,
		},
		{
			name: "cgroup v1 带 kubepods 字样",
			files: map[string]string{
				"/proc/1/cgroup": "12:pids:/kubepods/burstable/pod123/abc\n",
			},
			want: true,
		},
		{
			name: "cgroup v2 主机只有 0::/，靠 mountinfo 里的容器挂载兜底",
			files: map[string]string{
				"/proc/1/cgroup":       "0::/\n",
				"/proc/self/mountinfo": "1234 1233 0:99 / / rw,relatime - overlay overlay rw,lowerdir=/var/lib/docker/overlay2/l/ABC\n",
			},
			want: true,
		},
		{
			name: "K8s 注入的环境变量",
			env:  map[string]string{"KUBERNETES_SERVICE_HOST": "10.96.0.1"},
			want: true,
		},
		{
			name: "裸机（systemd / 绿色包）：全部信号都没有",
			files: map[string]string{
				"/proc/1/cgroup":       "0::/init.scope\n",
				"/proc/self/mountinfo": "1234 1233 8:1 / / rw,relatime - ext4 /dev/sda1 rw\n",
			},
			want: false,
		},
		{
			name:  "什么都读不到时判为非容器（容器提示不能误报）",
			files: map[string]string{},
			want:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectInContainer(probeOf(tc.files, tc.env)); got != tc.want {
				t.Fatalf("detectInContainer() = %v, want %v", got, tc.want)
			}
		})
	}
}
