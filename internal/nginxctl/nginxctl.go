package nginxctl

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"nginx-agent/internal/config"
	"nginx-agent/internal/fsops"
)

// NginxCtl 封装对本机 nginx 的命令操作：语法检测、reload、状态采集。
type NginxCtl struct {
	cfg config.NginxConfig
}

func New(cfg config.NginxConfig) *NginxCtl {
	return &NginxCtl{cfg: cfg}
}

// Result 表示一次命令执行结果。
type Result struct {
	OK     bool
	Output string
}

// Test 执行 nginx -t 语法检测。
func (n *NginxCtl) Test(ctx context.Context) (Result, error) {
	return n.runConfigured(ctx, n.cfg.TestCmd, n.cfg.Binary, "-t")
}

// Reload 执行 reload。
func (n *NginxCtl) Reload(ctx context.Context) (Result, error) {
	return n.runConfigured(ctx, n.cfg.ReloadCmd, n.cfg.Binary, "-s", "reload")
}

// runConfigured 优先使用配置里的命令字符串；为空则回退到 binary + 默认参数。
func (n *NginxCtl) runConfigured(ctx context.Context, configured, fallbackBin string, fallbackArgs ...string) (Result, error) {
	var name string
	var args []string
	if strings.TrimSpace(configured) != "" {
		if err := fsops.ValidateCommand(configured); err != nil {
			return Result{}, err
		}
		fields := strings.Fields(configured)
		name, args = fields[0], fields[1:]
	} else {
		name, args = fallbackBin, fallbackArgs
	}
	return n.run(ctx, name, args...)
}

func (n *NginxCtl) run(ctx context.Context, name string, args ...string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := buf.String()
	if err != nil {
		// nginx -t 失败时退出码非 0，但 output 才是有价值的信息
		return Result{OK: false, Output: out}, nil
	}
	return Result{OK: true, Output: out}, nil
}

var versionRe = regexp.MustCompile(`nginx version: (\S+)`)

// Version 返回 nginx 版本字符串（nginx -v 输出到 stderr）。
func (n *NginxCtl) Version(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, n.binary(), "-v")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	_ = cmd.Run()
	if m := versionRe.FindStringSubmatch(buf.String()); len(m) == 2 {
		return m[1], nil
	}
	return strings.TrimSpace(buf.String()), nil
}

func (n *NginxCtl) binary() string {
	if n.cfg.Binary != "" {
		return n.cfg.Binary
	}
	return "nginx"
}

// Status 描述 nginx 运行状态。
type Status struct {
	Running   bool
	Version   string
	MasterPID int
}

// Status 采集运行状态：通过 pgrep 查找 master 进程。
func (n *NginxCtl) Status(ctx context.Context) Status {
	st := Status{}
	if v, err := n.Version(ctx); err == nil {
		st.Version = v
	}
	pid, running := n.masterPID(ctx)
	st.Running = running
	st.MasterPID = pid
	return st
}

func (n *NginxCtl) masterPID(ctx context.Context) (int, bool) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// nginx: master process
	cmd := exec.CommandContext(ctx, "pgrep", "-f", "nginx: master")
	out, err := cmd.Output()
	if err != nil {
		return 0, false
	}
	lines := strings.Fields(strings.TrimSpace(string(out)))
	if len(lines) == 0 {
		return 0, false
	}
	pid, err := strconv.Atoi(lines[0])
	if err != nil {
		return 0, true
	}
	return pid, true
}

// Describe 返回便于日志的描述。
func (n *NginxCtl) Describe() string {
	return fmt.Sprintf("nginxctl{binary=%s, test=%q, reload=%q}", n.cfg.Binary, n.cfg.TestCmd, n.cfg.ReloadCmd)
}
