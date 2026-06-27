package server

import (
	"context"
	"fmt"

	"nginx-agent/internal/pb"
)

// 本地设置（快照保留份数、是否允许编辑主配置）回归为纯本地 config.yaml 配置，
// 不再支持中心远程修改。以下两个 RPC 仅为兼容 proto 接口保留：
// Get 只读返回当前（来自 config.yaml）的值；Update 一律拒绝。

// GetAgentSettings 只读返回当前生效的本地设置（来自 config.yaml）。
func (s *AgentServer) GetAgentSettings(_ context.Context, _ *pb.GetAgentSettingsRequest) (*pb.AgentSettingsReply, error) {
	return &pb.AgentSettingsReply{
		BackupRetain:          int32(s.cfg.Backup.Retain),
		AllowMainConfig:       s.cfg.Nginx.AllowMainConfig,
		AllowMainConfigRemote: false, // 远程修改能力已下线
	}, nil
}

// UpdateAgentSettings 已下线：这些设置请在该机器的 config.yaml 中修改并重启 Agent。
func (s *AgentServer) UpdateAgentSettings(_ context.Context, _ *pb.UpdateAgentSettingsRequest) (*pb.AgentSettingsReply, error) {
	return nil, fmt.Errorf("远程修改 Agent 设置已下线，请在该机器的 config.yaml 中修改（backup.retain / nginx.allow_main_config）并重启 Agent")
}

