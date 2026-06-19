package sourcecontrol

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	cryptossh "golang.org/x/crypto/ssh"
)

type GitClient struct {
	repoPath string
	repo     *gogit.Repository
}

func NewGitClient(repoPath string) (*GitClient, error) {
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		return nil, err
	}
	gitDir := filepath.Join(repoPath, ".git")
	var repo *gogit.Repository
	var err error
	if _, statErr := os.Stat(gitDir); errors.Is(statErr, os.ErrNotExist) {
		repo, err = gogit.PlainInit(repoPath, false)
	} else {
		repo, err = gogit.PlainOpen(repoPath)
	}
	if err != nil {
		return nil, err
	}
	return &GitClient{repoPath: repoPath, repo: repo}, nil
}

func CloneRepository(repoURL string, branch string, localPath string, auth transport.AuthMethod) (*GitClient, error) {
	if branch == "" {
		branch = "main"
	}
	repo, err := gogit.PlainClone(localPath, false, &gogit.CloneOptions{
		URL:           repoURL,
		Auth:          auth,
		ReferenceName: plumbing.NewBranchReferenceName(branch),
		SingleBranch:  true,
	})
	if err != nil {
		return nil, fmt.Errorf("git clone: %w", err)
	}
	return &GitClient{repoPath: localPath, repo: repo}, nil
}

func BuildAuth(cfg Config) (transport.AuthMethod, error) {
	if cfg.PrivateKey != "" {
		signer, err := gitssh.NewPublicKeys("git", []byte(cfg.PrivateKey), cfg.PrivateKeyPassphrase)
		if err != nil {
			return nil, fmt.Errorf("ssh auth: %w", err)
		}
		signer.HostKeyCallbackHelper = gitssh.HostKeyCallbackHelper{HostKeyCallback: cryptossh.InsecureIgnoreHostKey()}
		return signer, nil
	}
	if cfg.Username != "" && cfg.Password != "" {
		return &githttp.BasicAuth{Username: cfg.Username, Password: cfg.Password}, nil
	}
	if cfg.Password != "" {
		return &githttp.BasicAuth{Username: "token", Password: cfg.Password}, nil
	}
	return nil, nil
}

func (gc *GitClient) AddAll() error {
	worktree, err := gc.repo.Worktree()
	if err != nil {
		return err
	}
	return worktree.AddWithOptions(&gogit.AddOptions{All: true})
}

func (gc *GitClient) AddFile(path string) error {
	worktree, err := gc.repo.Worktree()
	if err != nil {
		return err
	}
	_, err = worktree.Add(filepath.ToSlash(path))
	return err
}

func (gc *GitClient) Commit(message string, authorName string, authorEmail string) (string, error) {
	if strings.TrimSpace(message) == "" {
		message = "chore: sync n8n resources"
	}
	if authorName == "" {
		authorName = "n8n Turbo"
	}
	if authorEmail == "" {
		authorEmail = "n8n-turbo@example.local"
	}
	worktree, err := gc.repo.Worktree()
	if err != nil {
		return "", err
	}
	hash, err := worktree.Commit(message, &gogit.CommitOptions{
		Author: &object.Signature{Name: authorName, Email: authorEmail, When: time.Now().UTC()},
	})
	if err != nil {
		return "", fmt.Errorf("git commit: %w", err)
	}
	return hash.String(), nil
}

func (gc *GitClient) Pull(branch string, auth transport.AuthMethod) error {
	if branch == "" {
		branch = "main"
	}
	worktree, err := gc.repo.Worktree()
	if err != nil {
		return err
	}
	err = worktree.Pull(&gogit.PullOptions{
		RemoteName:    "origin",
		ReferenceName: plumbing.NewBranchReferenceName(branch),
		Auth:          auth,
	})
	if errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return nil
	}
	return err
}

func (gc *GitClient) FetchAndReset(branch string, auth transport.AuthMethod) error {
	if branch == "" {
		branch = "main"
	}
	err := gc.repo.Fetch(&gogit.FetchOptions{RemoteName: "origin", Auth: auth})
	if err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return err
	}
	ref, err := gc.repo.Reference(plumbing.NewRemoteReferenceName("origin", branch), true)
	if err != nil {
		return err
	}
	worktree, err := gc.repo.Worktree()
	if err != nil {
		return err
	}
	return worktree.Reset(&gogit.ResetOptions{Commit: ref.Hash(), Mode: gogit.HardReset})
}

func (gc *GitClient) Push(branch string, auth transport.AuthMethod, force bool) error {
	if branch == "" {
		branch = "main"
	}
	specPrefix := ""
	if force {
		specPrefix = "+"
	}
	err := gc.repo.Push(&gogit.PushOptions{
		RemoteName: "origin",
		RefSpecs: []gogitconfig.RefSpec{
			gogitconfig.RefSpec(fmt.Sprintf("%srefs/heads/%s:refs/heads/%s", specPrefix, branch, branch)),
		},
		Auth:  auth,
		Force: force,
	})
	if errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return nil
	}
	return err
}

func (gc *GitClient) Status() (gogit.Status, error) {
	worktree, err := gc.repo.Worktree()
	if err != nil {
		return nil, err
	}
	return worktree.Status()
}

func (gc *GitClient) BehindAhead(branch string) (int, int, error) {
	if branch == "" {
		branch = "main"
	}
	localRef, err := gc.repo.Reference(plumbing.NewBranchReferenceName(branch), true)
	if err != nil {
		return 0, 0, nil
	}
	remoteRef, err := gc.repo.Reference(plumbing.NewRemoteReferenceName("origin", branch), true)
	if err != nil {
		return 0, 0, nil
	}
	if localRef.Hash() == remoteRef.Hash() {
		return 0, 0, nil
	}
	return 0, 1, nil
}
