package fsops

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nginx-agent/internal/config"
)

// FsOps 负责本机 nginx 配置文件的安全读写。
// 安全护栏：
//  1. 逻辑路径仅相对 config_root 解析，禁止 .. 越权；
//  2. 解析后的真实路径必须落在 allowed_paths 白名单内；
//  3. 默认禁止改写主配置 nginx.conf（除非配置显式放开）。
type FsOps struct {
	cfg config.NginxConfig
}

func New(cfg config.NginxConfig) *FsOps {
	return &FsOps{cfg: cfg}
}

// resolve 把逻辑路径（相对 config_root）解析为绝对真实路径，并做安全校验。
func (f *FsOps) resolve(logicalPath string) (string, error) {
	if logicalPath == "" {
		return "", fmt.Errorf("逻辑路径不能为空")
	}
	// 禁止绝对路径与上跳
	clean := filepath.Clean(logicalPath)
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("逻辑路径必须相对 config_root，不能是绝对路径: %s", logicalPath)
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("逻辑路径越权（包含 ..）: %s", logicalPath)
	}
	abs := filepath.Join(f.cfg.ConfigRoot, clean)
	abs, err := filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	// 必须落在白名单目录内
	if !f.withinAllowed(abs) {
		return "", fmt.Errorf("目标路径不在允许的白名单目录内: %s", abs)
	}
	return abs, nil
}

func (f *FsOps) withinAllowed(abs string) bool {
	for _, p := range f.cfg.AllowedPaths {
		ap, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(ap, abs)
		if err != nil {
			continue
		}
		if rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)) {
			return true
		}
	}
	return false
}

// isMainConfig 判断目标是否为主配置文件。
func (f *FsOps) isMainConfig(abs string) bool {
	name := f.cfg.MainConfigName
	if name == "" {
		name = "nginx.conf"
	}
	return filepath.Base(abs) == name &&
		filepath.Dir(abs) == filepath.Clean(f.cfg.ConfigRoot)
}

// Read 读取逻辑路径对应文件的内容与校验和。
func (f *FsOps) Read(logicalPath string) ([]byte, string, error) {
	abs, err := f.resolve(logicalPath)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, "", fmt.Errorf("读取 %s 失败: %w", logicalPath, err)
	}
	return data, Checksum(data), nil
}

// Write 写入内容。expectedChecksum 非空时做乐观锁校验。
// 返回写入后的新校验和。注意：本方法只做写入，不负责备份/回滚（由上层编排）。
func (f *FsOps) Write(logicalPath string, content []byte, expectedChecksum string) (string, error) {
	abs, err := f.resolve(logicalPath)
	if err != nil {
		return "", err
	}
	if f.isMainConfig(abs) && !f.cfg.AllowMainConfig {
		return "", fmt.Errorf("默认禁止改写主配置 %s（如需放开请设置 nginx.allow_main_config=true）", abs)
	}
	// 乐观锁
	if expectedChecksum != "" {
		if cur, err := os.ReadFile(abs); err == nil {
			if Checksum(cur) != expectedChecksum {
				return "", fmt.Errorf("文件已被他人修改（校验和不匹配），请刷新后重试")
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	if err := atomicWrite(abs, content, 0o644); err != nil {
		return "", fmt.Errorf("写入 %s 失败: %w", logicalPath, err)
	}
	return Checksum(content), nil
}

// Resolve 暴露给同包其他模块（如 snapshot）使用的安全解析。
func (f *FsOps) Resolve(logicalPath string) (string, error) {
	return f.resolve(logicalPath)
}

// ConfigRoot 返回配置主目录。
func (f *FsOps) ConfigRoot() string { return f.cfg.ConfigRoot }

// atomicWrite 通过临时文件 + rename 实现原子写入。
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".nginx-agent-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // 若 rename 成功则此处 remove 无副作用
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// Checksum 返回内容的 sha256 十六进制。
func Checksum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// 高危命令黑名单关键字。用于校验 nginx.test_cmd / reload_cmd 等配置，
// 防止配置被注入危险命令。
var dangerousPatterns = []string{
	"rm -rf /",
	"rm -fr /",
	"mkfs",
	"dd if=",
	":(){", // fork 炸弹
	"> /dev/sda",
	"chmod -R 777 /",
	"chmod 777 /",
	"mv / ",
	"shutdown",
	"reboot",
	"wget ", // 防止配置里下载执行
	"curl ",
}

// ValidateCommand 检查命令字符串是否命中高危黑名单。
func ValidateCommand(cmd string) error {
	low := strings.ToLower(strings.TrimSpace(cmd))
	for _, p := range dangerousPatterns {
		if strings.Contains(low, strings.ToLower(p)) {
			return fmt.Errorf("命令命中高危黑名单 %q，已拒绝执行: %s", p, cmd)
		}
	}
	return nil
}
