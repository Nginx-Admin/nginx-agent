package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"nginx-agent/internal/pb"
)

// agentOverride 是可被中心远程修改、并持久化到本地的运行时设置。
// 它叠加在 config.yaml 之上：启动时若 override 文件存在则覆盖对应项。
type agentOverride struct {
	BackupRetain    int   `json:"backup_retain,omitempty"`
	AllowMainConfig *bool `json:"allow_main_config,omitempty"` // 指针以区分"未设置"
}

var overrideMu sync.Mutex

// overridePath 返回 override 文件路径（放在备份目录同级）。
func (s *AgentServer) overridePath() string {
	return filepath.Join(filepath.Dir(s.cfg.Backup.Dir), "agent-settings.json")
}

// loadOverride 启动时加载本地 override 并叠加到运行时组件上。
func (s *AgentServer) loadOverride() {
	data, err := os.ReadFile(s.overridePath())
	if err != nil {
		return // 没有 override 文件，用 config.yaml 默认值
	}
	var ov agentOverride
	if err := json.Unmarshal(data, &ov); err != nil {
		log.Printf("override 文件解析失败，忽略: %v", err)
		return
	}
	if ov.BackupRetain > 0 {
		s.snap.SetRetain(ov.BackupRetain)
	}
	// allow_main_config 仅在本地总闸允许时才接受 override
	if ov.AllowMainConfig != nil && s.cfg.Nginx.AllowMainConfigRemote {
		s.fs.SetAllowMainConfig(*ov.AllowMainConfig)
	}
	log.Printf("已加载本地设置 override: retain=%d allowMain=%v(总闸=%v)",
		s.snap.Retain(), s.fs.AllowMainConfig(), s.cfg.Nginx.AllowMainConfigRemote)
}

// saveOverride 持久化当前可变设置到本地文件。
func (s *AgentServer) saveOverride() error {
	overrideMu.Lock()
	defer overrideMu.Unlock()
	allow := s.fs.AllowMainConfig()
	ov := agentOverride{
		BackupRetain:    s.snap.Retain(),
		AllowMainConfig: &allow,
	}
	data, err := json.MarshalIndent(ov, "", "  ")
	if err != nil {
		return err
	}
	path := s.overridePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// GetAgentSettings 返回当前生效的本地设置。
func (s *AgentServer) GetAgentSettings(_ context.Context, _ *pb.GetAgentSettingsRequest) (*pb.AgentSettingsReply, error) {
	return &pb.AgentSettingsReply{
		BackupRetain:          int32(s.snap.Retain()),
		AllowMainConfig:       s.fs.AllowMainConfig(),
		AllowMainConfigRemote: s.cfg.Nginx.AllowMainConfigRemote,
	}, nil
}

// UpdateAgentSettings 接收中心下发的设置变更，应用并持久化。
func (s *AgentServer) UpdateAgentSettings(_ context.Context, req *pb.UpdateAgentSettingsRequest) (*pb.AgentSettingsReply, error) {
	// 快照保留份数
	if req.GetBackupRetain() > 0 {
		s.snap.SetRetain(int(req.GetBackupRetain()))
	}
	// 主配置编辑开关：仅当本地总闸允许时才接受远程修改
	if s.cfg.Nginx.AllowMainConfigRemote {
		s.fs.SetAllowMainConfig(req.GetAllowMainConfig())
	} else if req.GetAllowMainConfig() {
		// 总闸关闭却想远程开启 → 拒绝并明确报错
		return nil, fmt.Errorf("本机未开启 allow_main_config_remote 总闸，拒绝远程开启主配置编辑")
	}

	if err := s.saveOverride(); err != nil {
		return nil, fmt.Errorf("持久化设置失败: %w", err)
	}
	log.Printf("中心更新本地设置 by %s: retain=%d allowMain=%v",
		req.GetActor(), s.snap.Retain(), s.fs.AllowMainConfig())

	return &pb.AgentSettingsReply{
		BackupRetain:          int32(s.snap.Retain()),
		AllowMainConfig:       s.fs.AllowMainConfig(),
		AllowMainConfigRemote: s.cfg.Nginx.AllowMainConfigRemote,
	}, nil
}
