package tasks

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// AnsibleRunner 把 Spec 翻译成实际要跑的 shell 命令
//
//	所有命令格式跟用户现有 Jenkins pipeline 对齐：
//	  rsync:          cd <ansible_root> && git pull && python <ansible_root>/<proj>/<proj>.py <service>
//	  update_version: cd <ansible_root> && git pull && ansible-playbook -i ... <service>.yaml -e deploy_version=...
//	  git_sync:       cd <ansible_root> && git pull
//
//	命令通过 bash -c 执行，让 cd / && / shell 展开都生效
type AnsibleRunner struct {
	AnsibleRoot string // /etc/ansible
}

func (r *AnsibleRunner) BuildCommand(s Spec) (*exec.Cmd, error) {
	switch s.Action {
	case ActionRsync:
		return r.buildRsync(s)
	case ActionUpdateVersion:
		return r.buildUpdateVersion(s)
	case ActionGitSync:
		return r.buildGitSync()
	default:
		return nil, fmt.Errorf("unknown action: %s", s.Action)
	}
}

func (r *AnsibleRunner) buildGitSync() (*exec.Cmd, error) {
	script := fmt.Sprintf(`set -e
cd %s
git checkout main
git reset --hard
git pull`, shQuote(r.AnsibleRoot))
	return exec.Command("/bin/bash", "-c", script), nil
}

func (r *AnsibleRunner) buildRsync(s Spec) (*exec.Cmd, error) {
	pyPath := filepath.Join(r.AnsibleRoot, s.Project, s.Project+".py")
	script := fmt.Sprintf(`set -e
cd %s
git checkout main
git reset --hard
git pull
sudo python %s %s`,
		shQuote(r.AnsibleRoot),
		shQuote(pyPath),
		shQuote(s.Service))
	return exec.Command("/bin/bash", "-c", script), nil
}

func (r *AnsibleRunner) buildUpdateVersion(s Spec) (*exec.Cmd, error) {
	inventory := filepath.Join(r.AnsibleRoot, "inventory", s.Project, s.Env,
		fmt.Sprintf("%s_%s_hosts", s.Project, s.Env))
	playbook := filepath.Join(r.AnsibleRoot, s.Project, s.Env, s.Service+".yaml")
	script := fmt.Sprintf(`set -e
cd %s
git checkout main
git reset --hard
git pull
sudo ansible-playbook -i %s %s -e deploy_version=%s --diff`,
		shQuote(r.AnsibleRoot),
		shQuote(inventory),
		shQuote(playbook),
		shQuote(s.Version))
	return exec.Command("/bin/bash", "-c", script), nil
}

// shQuote 把字符串包成单引号字符串供 bash 用，转义字符串内的单引号
//
//	bash 单引号内不解析任何转义；遇到内嵌的单引号用 '\''（结束当前引号 → 转义引号 → 重开引号）
func shQuote(s string) string {
	out := []byte{'\''}
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			out = append(out, '\'', '\\', '\'', '\'')
			continue
		}
		out = append(out, s[i])
	}
	out = append(out, '\'')
	return string(out)
}
