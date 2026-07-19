package sourcecontrol

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
)

type Service struct {
	config     *Config
	configRepo ConfigRepository
	gitClient  *GitClient
	exporter   *Exporter
	importer   *Importer
	repoPath   string
}

func NewService(repoPath string, configRepo ConfigRepository) (*Service, error) {
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		return nil, err
	}
	if configRepo == nil {
		configRepo = NewFileConfigRepository(filepath.Join(filepath.Dir(repoPath), filepath.Base(repoPath)+".source-control.json"))
	}
	return &Service{
		configRepo: configRepo,
		repoPath:   repoPath,
		exporter:   NewExporter(repoPath),
		importer:   NewImporter(repoPath),
	}, nil
}

// CurrentConfig returns the active configuration, or nil when not connected.
func (s *Service) CurrentConfig() *Config {
	return s.config
}

// Disconnect clears the stored connection.
func (s *Service) Disconnect(ctx context.Context) error {
	s.config = nil
	s.gitClient = nil
	return s.configRepo.Save(ctx, Config{})
}

func (s *Service) Connect(ctx context.Context, cfg Config) error {
	if cfg.Branch == "" {
		cfg.Branch = "main"
	}
	auth, err := BuildAuth(cfg)
	if err != nil {
		return err
	}
	var client *GitClient
	if cfg.RepoURL != "" {
		if _, err := os.Stat(filepath.Join(s.repoPath, ".git")); errors.Is(err, os.ErrNotExist) {
			client, err = CloneRepository(cfg.RepoURL, cfg.Branch, s.repoPath, auth)
		} else {
			client, err = NewGitClient(s.repoPath)
		}
	} else {
		client, err = NewGitClient(s.repoPath)
	}
	if err != nil {
		return err
	}
	cfg.Active = true
	s.config = &cfg
	s.gitClient = client
	return s.configRepo.Save(ctx, cfg)
}

func (s *Service) Load(ctx context.Context) error {
	cfg, err := s.configRepo.Load(ctx)
	if err != nil || cfg == nil {
		return err
	}
	s.config = cfg
	client, err := NewGitClient(s.repoPath)
	if err != nil {
		return err
	}
	s.gitClient = client
	return nil
}

func (s *Service) Push(ctx context.Context, deps PushDependencies, opts PushOptions) (*PushResult, error) {
	if s.gitClient == nil || s.config == nil {
		return nil, fmt.Errorf("source control is not connected")
	}
	files, err := s.exporter.ExportAll(deps)
	if err != nil {
		return nil, err
	}
	if len(opts.FileNames) == 0 {
		if err := s.gitClient.AddAll(); err != nil {
			return nil, err
		}
	} else {
		for _, file := range opts.FileNames {
			if err := s.gitClient.AddFile(file); err != nil {
				return nil, err
			}
		}
	}
	message := opts.Message
	if message == "" {
		message = "chore: sync n8n resources " + time.Now().UTC().Format("2006-01-02 15:04:05")
	}
	hash, err := s.gitClient.Commit(message, s.config.AuthorName, s.config.AuthorEmail)
	if err != nil {
		if strings.Contains(err.Error(), "empty") || strings.Contains(err.Error(), "nothing to commit") {
			return &PushResult{Status: "upToDate", Files: files}, nil
		}
		return nil, err
	}
	auth, err := BuildAuth(*s.config)
	if err != nil {
		return nil, err
	}
	if s.config.RepoURL != "" {
		if err := s.gitClient.Push(s.config.Branch, auth, opts.Force); err != nil {
			return nil, err
		}
	}
	return &PushResult{Status: "pushed", Files: files, Commit: hash}, nil
}

func (s *Service) Pull(ctx context.Context, force bool, target ImportTarget) (*PullResult, error) {
	if s.gitClient == nil || s.config == nil {
		return nil, fmt.Errorf("source control is not connected")
	}
	auth, err := BuildAuth(*s.config)
	if err != nil {
		return nil, err
	}
	if force {
		err = s.gitClient.FetchAndReset(s.config.Branch, auth)
	} else {
		err = s.gitClient.Pull(s.config.Branch, auth)
	}
	if err != nil {
		if errors.Is(err, gogit.NoErrAlreadyUpToDate) {
			return &PullResult{StatusCode: "upToDate"}, nil
		}
		if strings.Contains(strings.ToLower(err.Error()), "conflict") {
			return &PullResult{StatusCode: "conflict", Conflicts: []string{err.Error()}}, nil
		}
		return nil, err
	}
	result, err := s.importer.ImportAll()
	if err != nil {
		return nil, err
	}
	if target != nil {
		for _, row := range result.Workflows {
			if err := target.ApplyWorkflow(ctx, row); err != nil {
				return nil, fmt.Errorf("apply workflow %s: %w", row.ID, err)
			}
		}
		for _, row := range result.Variables {
			if err := target.ApplyVariable(ctx, row); err != nil {
				return nil, fmt.Errorf("apply variable %s: %w", row.ID, err)
			}
		}
	}
	return result, nil
}

func (s *Service) Status(ctx context.Context) (*StatusResult, error) {
	if s.gitClient == nil {
		return nil, fmt.Errorf("source control is not connected")
	}
	status, err := s.gitClient.Status()
	if err != nil {
		return nil, err
	}
	result := &StatusResult{}
	if s.config != nil {
		behind, ahead, err := s.gitClient.BehindAhead(s.config.Branch)
		if err == nil {
			result.Behind = behind
			result.Ahead = ahead
		}
	}
	for path, fileStatus := range status {
		file := SourceControlledFile{File: filepath.ToSlash(path), Status: gitStatusName(fileStatus.Staging, fileStatus.Worktree)}
		switch file.Status {
		case "new":
			result.Added = append(result.Added, file)
		case "deleted":
			result.Deleted = append(result.Deleted, file)
		case "conflict":
			file.Conflict = true
			result.Conflicted = append(result.Conflicted, file)
		case "untracked":
			result.Untracked = append(result.Untracked, file)
		default:
			result.Modified = append(result.Modified, file)
		}
	}
	return result, nil
}

func gitStatusName(staging gogit.StatusCode, worktree gogit.StatusCode) string {
	if staging == gogit.Untracked || worktree == gogit.Untracked {
		return "untracked"
	}
	if staging == gogit.Deleted || worktree == gogit.Deleted {
		return "deleted"
	}
	if staging == gogit.Added || worktree == gogit.Added {
		return "new"
	}
	if staging == gogit.UpdatedButUnmerged || worktree == gogit.UpdatedButUnmerged {
		return "conflict"
	}
	if staging == gogit.Unmodified && worktree == gogit.Unmodified {
		return "unchanged"
	}
	return "modified"
}
