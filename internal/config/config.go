package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 是 nginx-agent 的本地配置。所有目录差异在此吸收，
// 中心（nginx-admin）只发逻辑指令，由 Agent 翻译为本机实际路径与命令。
type Config struct {
	Agent  AgentConfig  `yaml:"agent"`
	Nginx  NginxConfig  `yaml:"nginx"`
	Backup BackupConfig `yaml:"backup"`
}

type AgentConfig struct {
	// gRPC 监听地址，仅内网/防火墙放行。
	Listen string    `yaml:"listen"`
	TLS    TLSConfig `yaml:"tls"`
}

type TLSConfig struct {
	// Enabled=false 时为开发期明文模式（生产务必开启 mTLS）。
	Enabled bool   `yaml:"enabled"`
	Cert    string `yaml:"cert"`
	Key     string `yaml:"key"`
	CA      string `yaml:"ca"`
}

type NginxConfig struct {
	// 本机配置主目录，如 /etc/nginx。
	ConfigRoot string `yaml:"config_root"`
	// nginx 二进制路径。
	Binary string `yaml:"binary"`
	// 语法检测命令，如 "/usr/sbin/nginx -t"。
	TestCmd string `yaml:"test_cmd"`
	// reload 命令，如 "systemctl reload nginx" 或 "/usr/sbin/nginx -s reload"。
	ReloadCmd string `yaml:"reload_cmd"`
	// 路径白名单：Agent 只允许读写这些目录下的文件。
	AllowedPaths []string `yaml:"allowed_paths"`
	// 主配置文件名（默认 nginx.conf），默认禁止改写，除非 AllowMainConfig=true。
	MainConfigName string `yaml:"main_config_name"`
	// 是否允许改写主配置（高危，默认 false）。可被中心远程修改（需 AllowMainConfigRemote=true）。
	AllowMainConfig bool `yaml:"allow_main_config"`
	// 安全总闸：是否允许中心远程修改 AllowMainConfig。默认 false——
	// 即使中心被攻破，也无法远程放开主配置编辑，除非本机显式打开此总闸。
	AllowMainConfigRemote bool `yaml:"allow_main_config_remote"`
}

type BackupConfig struct {
	// 本地备份目录。
	Dir string `yaml:"dir"`
	// 本地保留最近 N 份快照。
	Retain int `yaml:"retain"`
}

// Default 返回带合理默认值的配置。
func Default() Config {
	return Config{
		Agent: AgentConfig{
			Listen: "0.0.0.0:7443",
			TLS:    TLSConfig{Enabled: false},
		},
		Nginx: NginxConfig{
			ConfigRoot:      "/etc/nginx",
			Binary:          "/usr/sbin/nginx",
			TestCmd:         "/usr/sbin/nginx -t",
			ReloadCmd:       "/usr/sbin/nginx -s reload",
			AllowedPaths:    []string{"/etc/nginx"},
			MainConfigName:  "nginx.conf",
			AllowMainConfig: false,
		},
		Backup: BackupConfig{
			Dir:    "/data/nginx-agent/backups",
			Retain: 50,
		},
	}
}

// Load 从指定路径加载 YAML 配置，缺省字段使用 Default 填充。
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("读取配置文件 %s 失败: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("解析配置文件 %s 失败: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Validate 校验关键字段。
func (c Config) Validate() error {
	if c.Agent.Listen == "" {
		return fmt.Errorf("agent.listen 不能为空")
	}
	if c.Nginx.ConfigRoot == "" {
		return fmt.Errorf("nginx.config_root 不能为空")
	}
	if len(c.Nginx.AllowedPaths) == 0 {
		return fmt.Errorf("nginx.allowed_paths 不能为空（出于安全考虑必须显式声明可操作目录）")
	}
	if c.Agent.TLS.Enabled {
		if c.Agent.TLS.Cert == "" || c.Agent.TLS.Key == "" || c.Agent.TLS.CA == "" {
			return fmt.Errorf("启用 mTLS 时 agent.tls 的 cert/key/ca 均不能为空")
		}
	}
	if c.Nginx.MainConfigName == "" {
		c.Nginx.MainConfigName = "nginx.conf"
	}
	return nil
}
