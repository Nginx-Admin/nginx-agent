package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"nginx-agent/internal/config"
	"nginx-agent/internal/fsops"
)

// Snapshot 负责写入前的本地快照备份与回滚。
// 每个快照由一个 .conf 内容文件 + 一个 .meta.json 元数据文件组成，
// backup_ref 即不带后缀的文件名（时间戳 + 短哈希）。
type Snapshot struct {
	cfg config.BackupConfig
	fs  *fsops.FsOps
}

// SetRetain 动态调整快照保留份数（供中心远程设置）。
func (s *Snapshot) SetRetain(n int) {
	if n > 0 {
		s.cfg.Retain = n
	}
}

// Retain 返回当前保留份数。
func (s *Snapshot) Retain() int { return s.cfg.Retain }

func New(cfg config.BackupConfig, fs *fsops.FsOps) (*Snapshot, error) {
	if cfg.Dir == "" {
		return nil, fmt.Errorf("backup.dir 不能为空")
	}
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建备份目录失败: %w", err)
	}
	return &Snapshot{cfg: cfg, fs: fs}, nil
}

// Meta 是快照元数据。
type Meta struct {
	BackupRef   string `json:"backup_ref"`
	LogicalPath string `json:"logical_path"`
	Checksum    string `json:"checksum"`
	CreatedAt   int64  `json:"created_at_unix"`
	Note        string `json:"note"`
}

// Create 为逻辑路径当前内容创建一份快照。若文件不存在（新建场景）返回空 ref。
func (s *Snapshot) Create(logicalPath, note string) (Meta, error) {
	content, checksum, err := s.fs.Read(logicalPath)
	if err != nil {
		// 文件不存在时不算错误：新建文件无需备份
		if os.IsNotExist(err) || strings.Contains(err.Error(), "no such file") {
			return Meta{}, nil
		}
		return Meta{}, err
	}
	ref := fmt.Sprintf("%s_%s", time.Now().Format("20060102-150405.000"), shortHash(checksum))
	ref = sanitize(ref)

	contentPath := filepath.Join(s.cfg.Dir, ref+".conf")
	if err := os.WriteFile(contentPath, content, 0o644); err != nil {
		return Meta{}, fmt.Errorf("写快照内容失败: %w", err)
	}
	meta := Meta{
		BackupRef:   ref,
		LogicalPath: logicalPath,
		Checksum:    checksum,
		CreatedAt:   time.Now().Unix(),
		Note:        note,
	}
	if err := s.writeMeta(meta); err != nil {
		return Meta{}, err
	}
	s.prune(logicalPath)
	return meta, nil
}

func (s *Snapshot) writeMeta(meta Meta) error {
	metaPath := filepath.Join(s.cfg.Dir, meta.BackupRef+".meta.json")
	b, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(metaPath, b, 0o644); err != nil {
		return fmt.Errorf("写快照元数据失败: %w", err)
	}
	return nil
}

// List 列出指定逻辑路径（空则全部）的快照，按时间倒序。
func (s *Snapshot) List(logicalPath string) ([]Meta, error) {
	entries, err := os.ReadDir(s.cfg.Dir)
	if err != nil {
		return nil, err
	}
	var metas []Meta
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".meta.json") {
			continue
		}
		m, err := s.readMeta(strings.TrimSuffix(e.Name(), ".meta.json"))
		if err != nil {
			continue
		}
		if logicalPath != "" && m.LogicalPath != logicalPath {
			continue
		}
		metas = append(metas, m)
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].CreatedAt > metas[j].CreatedAt })
	return metas, nil
}

func (s *Snapshot) readMeta(ref string) (Meta, error) {
	b, err := os.ReadFile(filepath.Join(s.cfg.Dir, ref+".meta.json"))
	if err != nil {
		return Meta{}, err
	}
	var m Meta
	if err := json.Unmarshal(b, &m); err != nil {
		return Meta{}, err
	}
	return m, nil
}

// Content 返回某个快照的内容。
func (s *Snapshot) Content(ref string) ([]byte, Meta, error) {
	ref = sanitize(ref)
	m, err := s.readMeta(ref)
	if err != nil {
		return nil, Meta{}, fmt.Errorf("快照 %s 不存在: %w", ref, err)
	}
	content, err := os.ReadFile(filepath.Join(s.cfg.Dir, ref+".conf"))
	if err != nil {
		return nil, Meta{}, err
	}
	return content, m, nil
}

// prune 保留某逻辑路径最近 retain 份，删除更旧的。
func (s *Snapshot) prune(logicalPath string) {
	if s.cfg.Retain <= 0 {
		return
	}
	metas, err := s.List(logicalPath)
	if err != nil {
		return
	}
	for i, m := range metas {
		if i < s.cfg.Retain {
			continue
		}
		os.Remove(filepath.Join(s.cfg.Dir, m.BackupRef+".conf"))
		os.Remove(filepath.Join(s.cfg.Dir, m.BackupRef+".meta.json"))
	}
}

func shortHash(checksum string) string {
	if len(checksum) >= 8 {
		return checksum[:8]
	}
	return checksum
}

// sanitize 防止 backup_ref 被注入路径分隔符导致越权。
func sanitize(ref string) string {
	ref = filepath.Base(ref)
	ref = strings.ReplaceAll(ref, "..", "")
	return ref
}
