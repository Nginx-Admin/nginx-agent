package server

import (
	"context"
	"fmt"
	"time"

	"nginx-agent/internal/config"
	"nginx-agent/internal/discover"
	"nginx-agent/internal/fsops"
	"nginx-agent/internal/nginxctl"
	"nginx-agent/internal/pb"
	"nginx-agent/internal/snapshot"
)

// Version 是 agent 版本号（构建时可注入）。
var Version = "0.1.0-dev"

// AgentServer 实现 pb.AgentServiceServer。
type AgentServer struct {
	pb.UnimplementedAgentServiceServer

	cfg  config.Config
	fs   *fsops.FsOps
	nx   *nginxctl.NginxCtl
	snap *snapshot.Snapshot
	disc *discover.Discover
}

func NewAgentServer(cfg config.Config) (*AgentServer, error) {
	fs := fsops.New(cfg.Nginx)
	snap, err := snapshot.New(cfg.Backup, fs)
	if err != nil {
		return nil, err
	}
	return &AgentServer{
		cfg:  cfg,
		fs:   fs,
		nx:   nginxctl.New(cfg.Nginx),
		snap: snap,
		disc: discover.New(cfg.Nginx, fs),
	}, nil
}

func (s *AgentServer) Ping(_ context.Context, _ *pb.PingRequest) (*pb.PingReply, error) {
	return &pb.PingReply{AgentVersion: Version, ServerTimeUnix: time.Now().Unix()}, nil
}

func (s *AgentServer) GetStatus(ctx context.Context, _ *pb.StatusRequest) (*pb.StatusReply, error) {
	st := s.nx.Status(ctx)
	test, _ := s.nx.Test(ctx)
	return &pb.StatusReply{
		NginxRunning:   st.Running,
		NginxVersion:   st.Version,
		MasterPid:      int32(st.MasterPID),
		ConfigRoot:     s.fs.ConfigRoot(),
		LastTestOk:     test.OK,
		LastTestOutput: test.Output,
	}, nil
}

func (s *AgentServer) DiscoverConfigs(_ context.Context, _ *pb.DiscoverRequest) (*pb.DiscoverReply, error) {
	res := s.disc.Scan()
	reply := &pb.DiscoverReply{ServerNames: res.ServerNames}
	for _, f := range res.Files {
		reply.Files = append(reply.Files, &pb.ConfigFile{
			LogicalPath: f.LogicalPath,
			Size:        f.Size,
			MtimeUnix:   f.MtimeUnix,
			Checksum:    f.Checksum,
		})
	}
	return reply, nil
}

func (s *AgentServer) ListConfigs(ctx context.Context, _ *pb.ListConfigsRequest) (*pb.ListConfigsReply, error) {
	res := s.disc.Scan()
	reply := &pb.ListConfigsReply{}
	for _, f := range res.Files {
		reply.Files = append(reply.Files, &pb.ConfigFile{
			LogicalPath: f.LogicalPath,
			Size:        f.Size,
			MtimeUnix:   f.MtimeUnix,
			Checksum:    f.Checksum,
		})
	}
	return reply, nil
}

func (s *AgentServer) ReadConfig(_ context.Context, req *pb.ReadConfigRequest) (*pb.ReadConfigReply, error) {
	content, checksum, err := s.fs.Read(req.GetLogicalPath())
	if err != nil {
		return nil, err
	}
	return &pb.ReadConfigReply{Content: content, Checksum: checksum}, nil
}

// WriteConfig 执行安全闭环：快照 → 写入 → nginx -t → 通过则 reload；失败则回滚。
func (s *AgentServer) WriteConfig(ctx context.Context, req *pb.WriteConfigRequest) (*pb.WriteConfigReply, error) {
	logical := req.GetLogicalPath()

	// 1. 写入前快照（若开启）
	var backupRef string
	if req.GetAutoBackup() {
		note := fmt.Sprintf("write 前自动备份 by %s", req.GetActor())
		meta, err := s.snap.Create(logical, note)
		if err != nil {
			return &pb.WriteConfigReply{Ok: false, Error: "备份失败: " + err.Error()}, nil
		}
		backupRef = meta.BackupRef
	}

	// 2. 写入新内容（带乐观锁）
	newChecksum, err := s.fs.Write(logical, req.GetContent(), req.GetExpectedChecksum())
	if err != nil {
		return &pb.WriteConfigReply{Ok: false, BackupRef: backupRef, Error: err.Error()}, nil
	}

	// 3. nginx -t 校验
	test, _ := s.nx.Test(ctx)
	if !test.OK {
		// 回滚到快照
		s.restore(backupRef, logical)
		return &pb.WriteConfigReply{
			Ok:        false,
			BackupRef: backupRef,
			Error:     "nginx -t 校验失败，已回滚:\n" + test.Output,
		}, nil
	}

	// 4. reload
	reload, _ := s.nx.Reload(ctx)
	if !reload.OK {
		s.restore(backupRef, logical)
		// 回滚后再 reload 一次，尽量恢复服务
		s.nx.Reload(ctx)
		return &pb.WriteConfigReply{
			Ok:        false,
			BackupRef: backupRef,
			Error:     "reload 失败，已回滚:\n" + reload.Output,
		}, nil
	}

	return &pb.WriteConfigReply{Ok: true, BackupRef: backupRef, NewChecksum: newChecksum}, nil
}

// restore 用快照内容覆盖回目标文件（用于回滚）。backupRef 为空表示原本是新建文件，直接删除。
func (s *AgentServer) restore(backupRef, logical string) {
	if backupRef == "" {
		// 新建场景：删除刚写入的文件
		if abs, err := s.fs.Resolve(logical); err == nil {
			_ = removeFile(abs)
		}
		return
	}
	content, _, err := s.snap.Content(backupRef)
	if err != nil {
		return
	}
	// 回滚写入不走乐观锁
	_, _ = s.fs.Write(logical, content, "")
}

func (s *AgentServer) TestConfig(ctx context.Context, _ *pb.TestConfigRequest) (*pb.TestConfigReply, error) {
	res, err := s.nx.Test(ctx)
	if err != nil {
		return nil, err
	}
	return &pb.TestConfigReply{Ok: res.OK, Output: res.Output}, nil
}

func (s *AgentServer) Reload(ctx context.Context, _ *pb.ReloadRequest) (*pb.ReloadReply, error) {
	res, err := s.nx.Reload(ctx)
	if err != nil {
		return nil, err
	}
	return &pb.ReloadReply{Ok: res.OK, Output: res.Output}, nil
}

func (s *AgentServer) ListBackups(_ context.Context, req *pb.ListBackupsRequest) (*pb.ListBackupsReply, error) {
	metas, err := s.snap.List(req.GetLogicalPath())
	if err != nil {
		return nil, err
	}
	reply := &pb.ListBackupsReply{}
	for _, m := range metas {
		reply.Backups = append(reply.Backups, &pb.Backup{
			BackupRef:     m.BackupRef,
			LogicalPath:   m.LogicalPath,
			Checksum:      m.Checksum,
			CreatedAtUnix: m.CreatedAt,
			Note:          m.Note,
		})
	}
	return reply, nil
}

// Rollback 回滚到指定快照：快照内容 → 写入 → nginx -t → reload。
func (s *AgentServer) Rollback(ctx context.Context, req *pb.RollbackRequest) (*pb.RollbackReply, error) {
	content, meta, err := s.snap.Content(req.GetBackupRef())
	if err != nil {
		return &pb.RollbackReply{Ok: false, Error: err.Error()}, nil
	}
	// 回滚前再快照一次当前状态，便于"回滚的回滚"
	_, _ = s.snap.Create(meta.LogicalPath, "rollback 前自动备份 by "+req.GetActor())

	if _, err := s.fs.Write(meta.LogicalPath, content, ""); err != nil {
		return &pb.RollbackReply{Ok: false, Error: err.Error()}, nil
	}
	test, _ := s.nx.Test(ctx)
	if !test.OK {
		return &pb.RollbackReply{Ok: false, Output: test.Output, Error: "回滚后 nginx -t 失败"}, nil
	}
	reload, _ := s.nx.Reload(ctx)
	if !reload.OK {
		return &pb.RollbackReply{Ok: false, Output: reload.Output, Error: "回滚后 reload 失败"}, nil
	}
	return &pb.RollbackReply{Ok: true, Output: test.Output}, nil
}
