//go:build unix

package processo

import (
	"context"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestAlvoDoKillEvitaSuicidio é a guarda mais importante do pacote: se o Setpgid não valer,
// o filho herda o grupo do servidor e kill(-pgid) mata o SERVIDOR. Sintoma: o serviço morre
// ao cancelar um download, sem rastro que aponte para cá.
func TestAlvoDoKillEvitaSuicidio(t *testing.T) {
	casos := []struct {
		nome                           string
		pidFilho, pgidFilho, pgidAtual int
		querAlvo                       int
		querAviso                      string
	}{
		{
			nome:     "caminho normal: filho lidera grupo próprio, mata o grupo",
			pidFilho: 500, pgidFilho: 500, pgidAtual: 100,
			querAlvo: -500,
		},
		{
			nome:     "Setpgid não valeu: filho no grupo do servidor, NÃO mata o grupo",
			pidFilho: 500, pgidFilho: 100, pgidAtual: 100,
			querAlvo: 500, querAviso: "grupo do próprio servidor",
		},
		{
			nome:     "filho não lidera o grupo: não atinge terceiros",
			pidFilho: 500, pgidFilho: 400, pgidAtual: 100,
			querAlvo: 500, querAviso: "não lidera o grupo",
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			alvo, aviso := alvoDoKill(c.pidFilho, c.pgidFilho, c.pgidAtual)
			if alvo != c.querAlvo {
				t.Errorf("alvo = %d, queria %d", alvo, c.querAlvo)
			}
			if alvo > 0 && aviso == "" {
				t.Error("degradou para PID direto sem avisar — falha silenciosa")
			}
			if c.querAviso != "" && !strings.Contains(aviso, c.querAviso) {
				t.Errorf("aviso não explica o motivo (%q): %q", c.querAviso, aviso)
			}
		})
	}
}

// TestNuncaMataOGrupoDoServidor é a asserção absoluta: para QUALQUER combinação, o alvo
// negativo jamais pode ser o grupo do processo atual.
func TestNuncaMataOGrupoDoServidor(t *testing.T) {
	const meuPgid = 4242
	for _, pgidFilho := range []int{meuPgid, 1, 4241, 4243, 999999} {
		for _, pidFilho := range []int{meuPgid, 1, 4241, 4243, 999999} {
			alvo, _ := alvoDoKill(pidFilho, pgidFilho, meuPgid)
			if alvo == -meuPgid {
				t.Fatalf("pid=%d pgid=%d: alvo seria o grupo do servidor (suicídio)", pidFilho, pgidFilho)
			}
		}
	}
}

// TestFilhoRealFicaEmGrupoProprio confirma que, no caminho de verdade, o Setpgid VALE — a
// guarda acima é rede de segurança, não o comportamento esperado. Se este teste falhar, o
// kill de grupo degradou para PID direto e os netos voltam a sobreviver.
func TestFilhoRealFicaEmGrupoProprio(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "sh", "-c", "sleep 5")
	isolarGrupo(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cmd.Wait()
	}()

	pgidFilho, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	pgidAtual, err := syscall.Getpgid(0)
	if err != nil {
		t.Fatal(err)
	}
	if pgidFilho == pgidAtual {
		t.Fatal("o filho ficou no grupo do servidor: o Setpgid não valeu")
	}
	if pgidFilho != cmd.Process.Pid {
		t.Errorf("o filho não lidera o próprio grupo (pgid %d, pid %d)", pgidFilho, cmd.Process.Pid)
	}
	if alvo, aviso := alvoDoKill(cmd.Process.Pid, pgidFilho, pgidAtual); alvo >= 0 {
		t.Errorf("no caminho normal deveria matar o grupo, mas degradou: alvo=%d (%s)", alvo, aviso)
	}
}

// TestSIGKILLMataProcessoParado: um yt-dlp travado por kill -STOP é o caso real de
// "travado". SIGTERM não é entregue a processo parado; SIGKILL é. O executor usa SIGKILL —
// este teste trava essa escolha.
func TestSIGKILLMataProcessoParado(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "sh", "-c", "sleep 30")
	isolarGrupo(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	if err := syscall.Kill(pid, syscall.SIGSTOP); err != nil {
		t.Fatalf("não consegui parar o processo: %v", err)
	}
	if err := cmd.Cancel(); err != nil {
		t.Fatalf("cancelamento falhou com o processo parado: %v", err)
	}
	cmd.Wait()
	if !esperarMorrer(t, pid, 3*time.Second) {
		t.Fatal("processo em estado STOPPED sobreviveu ao cancelamento")
	}
}
