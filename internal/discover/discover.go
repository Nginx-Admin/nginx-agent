package discover

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"nginx-agent/internal/config"
	"nginx-agent/internal/fsops"
)

// Discover 在 Agent 接入时盘点本机已有的 nginx 配置文件。
// 扫描范围：主配置 nginx.conf、conf.d/*.conf、sites-enabled/*（兼容 sites-available/enabled 布局）。
type Discover struct {
	cfg config.NginxConfig
	fs  *fsops.FsOps
}

func New(cfg config.NginxConfig, fs *fsops.FsOps) *Discover {
	return &Discover{cfg: cfg, fs: fs}
}

// File 是发现到的配置文件信息。
type File struct {
	LogicalPath string
	Size        int64
	MtimeUnix   int64
	Checksum    string
}

// Result 是一次发现的结果。
type Result struct {
	Files       []File
	ServerNames []string
}

var serverNameRe = regexp.MustCompile(`(?m)^\s*server_name\s+([^;]+);`)

// Scan 执行配置发现。
func (d *Discover) Scan() Result {
	root := d.cfg.ConfigRoot
	candidates := map[string]struct{}{}

	// 主配置
	mainName := d.cfg.MainConfigName
	if mainName == "" {
		mainName = "nginx.conf"
	}
	addIfFile(candidates, filepath.Join(root, mainName))

	// conf.d/*.conf
	for _, p := range globConf(filepath.Join(root, "conf.d", "*.conf")) {
		candidates[p] = struct{}{}
	}
	// sites-enabled/*（可能是软链）
	for _, p := range globAny(filepath.Join(root, "sites-enabled", "*")) {
		candidates[p] = struct{}{}
	}

	var res Result
	nameSet := map[string]struct{}{}

	for abs := range candidates {
		info, err := os.Stat(abs) // Stat 跟随软链
		if err != nil || info.IsDir() {
			continue
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			continue
		}
		content, checksum, err := d.fs.Read(rel)
		if err != nil {
			// 不在白名单或读不了的跳过
			continue
		}
		res.Files = append(res.Files, File{
			LogicalPath: rel,
			Size:        info.Size(),
			MtimeUnix:   info.ModTime().Unix(),
			Checksum:    checksum,
		})
		for _, m := range serverNameRe.FindAllStringSubmatch(string(content), -1) {
			for _, name := range strings.Fields(m[1]) {
				name = strings.TrimSpace(name)
				if name != "" && name != "_" {
					nameSet[name] = struct{}{}
				}
			}
		}
	}

	sort.Slice(res.Files, func(i, j int) bool { return res.Files[i].LogicalPath < res.Files[j].LogicalPath })
	for n := range nameSet {
		res.ServerNames = append(res.ServerNames, n)
	}
	sort.Strings(res.ServerNames)
	return res
}

func addIfFile(set map[string]struct{}, path string) {
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		set[path] = struct{}{}
	}
}

func globConf(pattern string) []string {
	m, _ := filepath.Glob(pattern)
	return m
}

func globAny(pattern string) []string {
	m, _ := filepath.Glob(pattern)
	return m
}
