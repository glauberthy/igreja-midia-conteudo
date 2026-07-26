//go:build unix

package processo

import (
	"fmt"
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
		return matar(cmd.Process.Pid)
	}
}

// matar envia SIGKILL ao grupo do filho — mas só depois de PROVAR que aquele grupo não é o
// nosso. Ver alvoDoKill: kill(-pgid) num pgid herdado do servidor derruba o servidor.
//
// SIGKILL, e não SIGTERM, de propósito: o caso que interessa é o processo TRAVADO, e a um
// processo parado (SIGSTOP, que é como um stall se comporta) o SIGTERM não é entregue.
func matar(pid int) error {
	pgidFilho, err := syscall.Getpgid(pid)
	if err != nil {
		// Sem saber o grupo, não se arrisca o sinal em grupo: mata só o processo direto.
		return syscall.Kill(pid, syscall.SIGKILL)
	}
	pgidAtual, err := syscall.Getpgid(0) // 0 = o processo atual
	if err != nil {
		return syscall.Kill(pid, syscall.SIGKILL)
	}
	alvo, aviso := alvoDoKill(pid, pgidFilho, pgidAtual)
	if aviso != "" {
		Avisar(aviso)
	}
	return syscall.Kill(alvo, syscall.SIGKILL)
}

// alvoDoKill decide QUEM recebe o SIGKILL. Separada da syscall para ser testável sem
// processos — a decisão errada aqui derruba o servidor de um jeito quase impossível de
// diagnosticar (o serviço simplesmente morre ao cancelar um download).
//
// Alvo negativo = o grupo; positivo = só aquele processo. Recusa o sinal em grupo em dois
// casos, ambos indicando que o Setpgid não valeu (outra construção do comando, refactor,
// falha silenciosa da syscall):
//
//   - o grupo do filho é o NOSSO: kill(-pgid) seria suicídio do servidor;
//   - o filho não é o líder do próprio grupo: o grupo é de terceiros, e matá-lo atingiria
//     processos que não criamos.
//
// Nos dois casos degrada para matar só o PID direto e AVISA — perder um neto é ruim
// (espaço em disco preso), derrubar o serviço é pior, e o aviso deixa rastro para o
// diagnóstico em vez de falhar em silêncio.
func alvoDoKill(pidFilho, pgidFilho, pgidAtual int) (alvo int, aviso string) {
	if pgidFilho == pgidAtual {
		return pidFilho, fmt.Sprintf(
			"aviso: o processo %d ficou no grupo do próprio servidor (%d) — matando só ele, "+
				"não o grupo; netos podem sobreviver e reter espaço em disco", pidFilho, pgidAtual)
	}
	if pgidFilho != pidFilho {
		return pidFilho, fmt.Sprintf(
			"aviso: o processo %d não lidera o grupo %d — matando só ele, para não atingir "+
				"processos de terceiros", pidFilho, pgidFilho)
	}
	return -pgidFilho, ""
}
