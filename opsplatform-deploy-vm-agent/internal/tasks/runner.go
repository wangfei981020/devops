package tasks

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	// rsync 走 python 脚本，跟 ansible 没关系，sudo 不用透传 ANSIBLE_*
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

	// agent 进程自己有 ANSIBLE_LOCAL_TEMP / ANSIBLE_REMOTE_TEMP（systemd Environment 注入），
	// 但 sudo 默认会剥光 env，所以要在 sudo 命令行里显式重申，写成 `sudo VAR=value cmd` 形式。
	// （这要 sudoers 没 `Defaults !setenv`，绝大多数发行版默认允许）
	envPrefix := buildAnsibleEnvPrefix()

	script := fmt.Sprintf(`set -e
cd %s
git checkout main
git reset --hard
git pull
sudo %sansible-playbook -i %s %s -e deploy_version=%s --diff`,
		shQuote(r.AnsibleRoot),
		envPrefix,
		shQuote(inventory),
		shQuote(playbook),
		shQuote(s.Version))
	return exec.Command("/bin/bash", "-c", script), nil
}

// buildAnsibleEnvPrefix 读 agent 进程自己的 ANSIBLE_* 环境变量，拼成 sudo 命令前缀。
//
//	返回形如 `ANSIBLE_LOCAL_TEMP='/data/xxx' ANSIBLE_REMOTE_TEMP='/tmp' `（末尾带空格），
//	让调用方拼到 `sudo <prefix>ansible-playbook ...` 中间。没有任何相关 env 时返空串。
//	目前只透传：
//	  ANSIBLE_LOCAL_TEMP   控制机临时目录（绕开 systemd ProtectHome=read-only 锁住的 ~/.ansible）
//	  ANSIBLE_REMOTE_TEMP  目标 VM 临时目录（一般不用设，留默认即可；用户设了我们不挡）
func buildAnsibleEnvPrefix() string {
	keys := []string{"ANSIBLE_LOCAL_TEMP", "ANSIBLE_REMOTE_TEMP"}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			parts = append(parts, k+"="+shQuote(v))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ") + " "
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
