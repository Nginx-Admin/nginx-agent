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
//
// 发现策略（按 nginx 真实加载逻辑）：
//  1. 主配置 nginx.conf；
//  2. 解析主配置中的 include 指令，递归展开（glob + 递归 include），
//     得到 nginx 实际加载的全部子配置——不依赖固定目录名，兼容
//     openresty 等非标准布局（conf.d 与 config_root 同级等）；
//  3. 兜底再扫常见目录 conf.d/*.conf、sites-enabled/*，防止 include 写法特殊时漏掉。
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

var (
	serverNameRe = regexp.MustCompile(`(?m)^\s*server_name\s+([^;]+);`)
	// 匹配 include 指令：include path; （path 可带引号）
	includeRe = regexp.MustCompile(`(?m)^\s*include\s+["']?([^"';]+)["']?\s*;`)
)

// Scan 执行配置发现。
func (d *Discover) Scan() Result {
	root := d.cfg.ConfigRoot
	mainName := d.cfg.MainConfigName
	if mainName == "" {
		mainName = "nginx.conf"
	}
	// 主配置绝对路径：mainName 可能是 "nginx.conf" 或带相对子路径
	mainAbs := mainName
	if !filepath.IsAbs(mainAbs) {
		mainAbs = filepath.Join(root, mainName)
	}

	candidates := map[string]struct{}{}
	addIfFile(candidates, mainAbs)

	// 解析 include，递归收集子配置（以主配置所在目录为相对基准）
	visited := map[string]bool{}
	d.collectIncludes(mainAbs, filepath.Dir(mainAbs), candidates, visited)

	// 兜底：常见目录（include 写法特殊时补漏）
	for _, p := range glob(filepath.Join(root, "conf.d", "*.conf")) {
		candidates[p] = struct{}{}
	}
	for _, p := range glob(filepath.Join(root, "sites-enabled", "*")) {
		candidates[p] = struct{}{}
	}

	var res Result
	nameSet := map[string]struct{}{}

	for abs := range candidates {
		info, err := os.Stat(abs) // Stat 跟随软链
		if err != nil || info.IsDir() {
			continue
		}
		logical := d.toLogical(abs)
		content, checksum, err := d.fs.Read(logical)
		if err != nil {
			// 不在白名单或读不了的跳过
			continue
		}
		res.Files = append(res.Files, File{
			LogicalPath: logical,
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

// collectIncludes 读取 confPath，解析其中的 include 指令，glob 展开后加入 candidates，
// 并对展开到的 .conf 文件递归解析其 include（防循环）。
// baseDir 为相对 include 的基准目录（nginx 以包含该 include 的文件所在目录为基准）。
func (d *Discover) collectIncludes(confPath, baseDir string, candidates map[string]struct{}, visited map[string]bool) {
	if visited[confPath] {
		return
	}
	visited[confPath] = true

	data, err := os.ReadFile(confPath)
	if err != nil {
		return
	}
	for _, m := range includeRe.FindAllStringSubmatch(string(data), -1) {
		pat := strings.TrimSpace(m[1])
		if pat == "" {
			continue
		}
		if !filepath.IsAbs(pat) {
			pat = filepath.Join(baseDir, pat)
		}
		for _, p := range glob(pat) {
			info, err := os.Stat(p)
			if err != nil || info.IsDir() {
				continue
			}
			candidates[p] = struct{}{}
			// 递归：被 include 的文件里可能还有 include
			d.collectIncludes(p, filepath.Dir(p), candidates, visited)
		}
	}
}

// toLogical 把绝对路径转为对外的逻辑路径（相对 config_root，可能含 ..）。
func (d *Discover) toLogical(abs string) string {
	rel, err := filepath.Rel(d.cfg.ConfigRoot, abs)
	if err != nil {
		return abs // 退化为绝对路径，fsops 白名单仍会校验
	}
	return rel
}

func addIfFile(set map[string]struct{}, path string) {
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		set[path] = struct{}{}
	}
}

func glob(pattern string) []string {
	m, _ := filepath.Glob(pattern)
	return m
}
