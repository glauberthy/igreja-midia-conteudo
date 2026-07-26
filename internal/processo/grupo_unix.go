//go:build unix

package processo

import (
	"os/exec"
	"syscall"
)

// isolarGrupo põe o comando num grupo de processos novo e troca o cancelamento padrão
// (que mata só o processo direto) por um SIGKILL no grupo — o PID negativo alcança os
// filhos que o comando criou. É isto que impede o ffmpeg neto do yt-dlp de sobreviver
// segurando o descritor do arquivo parcial.
func isolarGrupo(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// -pid = "o grupo cujo líder é pid". O Setpgid acima garante que o líder é ele.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
