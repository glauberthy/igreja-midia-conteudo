//go:build unix

package servidor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"srtclean/internal/download"
	"srtclean/internal/pipeline"
)

// TestExperimentoRealDownloadTravado fecha a lacuna de "teoria testada": até aqui o prazo e
// o kill de grupo só rodaram contra fakes. Aqui os três elos são exercitados contra um
// yt-dlp DE VERDADE, com a árvore de processos e os descritores reais.
//
// "Travado" não precisa de rede ruim: kill -STOP reproduz o estado exatamente — o processo
// para de escrever e não morre. É também o caso que distingue SIGKILL de SIGTERM (a um
// processo parado, SIGTERM não é entregue).
//
// Não roda por padrão: baixa vídeo real e consome cota do YouTube. Para rodar:
//
//	SHORTS_EXPERIMENTO_REAL=1 SHORTS_EXPERIMENTO_URL=<url> go test -run ExperimentoReal \
//	    -timeout 10m -v ./internal/servidor/
func TestExperimentoRealDownloadTravado(t *testing.T) {
	if os.Getenv("SHORTS_EXPERIMENTO_REAL") != "1" {
		t.Skip("experimento com rede: defina SHORTS_EXPERIMENTO_REAL=1 para rodar")
	}
	url := os.Getenv("SHORTS_EXPERIMENTO_URL")
	if url == "" {
		t.Skip("defina SHORTS_EXPERIMENTO_URL com o vídeo a usar")
	}

	base := t.TempDir()
	dir := filepath.Join(base, "exp-1")

	b := download.NovoBaixador()
	b.BaseDir = base
	b.Formato = download.FormatoPadrao
	ped := &pipeline.Pedido{ID: "exp-1", YouTubeURL: url}

	const semProgresso = 25 * time.Second
	pararQuandoCrescer(t, dir, 4<<20)

	inicio := time.Now()
	err := etapaComProgresso(context.Background(), "o download do vídeo", dir,
		semProgresso, 5*time.Minute,
		func(ctx context.Context) error { _, e := b.BaixarVideoCompleto(ctx, ped); return e })
	decorrido := time.Since(inicio)

	// ELO 1 — o watchdog disparou por falta de progresso (e não pelo teto).
	if err == nil {
		t.Fatal("o download terminou: o SIGSTOP não pegou; experimento inconclusivo")
	}
	t.Logf("abortado em %s com: %v", decorrido.Round(time.Second), err)
	if !strings.Contains(err.Error(), "sem baixar nada") {
		t.Errorf("ELO 1 falhou: abortou por outro motivo, não por falta de progresso: %v", err)
	}

	// ELO 2 — nada da árvore do yt-dlp sobreviveu, inclusive estando PARADO.
	if vivos := pidsVivos(t, "yt-dlp"); len(vivos) > 0 {
		for _, p := range vivos {
			syscall.Kill(p, syscall.SIGKILL)
		}
		t.Errorf("ELO 2 falhou: %d processo(s) yt-dlp sobreviveram: %v", len(vivos), vivos)
	}
	if vivos := pidsVivos(t, "ffmpeg"); len(vivos) > 0 {
		for _, p := range vivos {
			syscall.Kill(p, syscall.SIGKILL)
		}
		t.Errorf("ELO 2 falhou: %d ffmpeg neto(s) sobreviveram: %v", len(vivos), vivos)
	}

	// ELO 3 — o espaço volta ao FILESYSTEM, não só o arquivo desaparece.
	parcial := tamanhoPasta(dir)
	t.Logf("resíduo em disco antes da limpeza: %d MB", parcial>>20)
	if parcial == 0 {
		t.Fatal("nada foi baixado; experimento inconclusivo")
	}

	// 3a — a CAUSA, medida direto: ninguém mantém descritor aberto para a pasta. É a
	// verificação determinística; o statfs abaixo é o efeito.
	if donos := fdsAbertosEm(t, dir); len(donos) > 0 {
		t.Errorf("ELO 3 falhou: descritores ainda abertos na pasta do pedido: %v", donos)
	}

	// 3b — o EFEITO: o filesystem devolveu os blocos.
	//
	// Medido como DELTA da remoção (livre depois - livre antes dela), e não contra o livre
	// do começo do teste. A primeira tentativa fez isso e acusou "75 MB não devolvidos"
	// com resíduo de 9 MB — impossível: o que ela mediu foi a atividade normal da máquina
	// ao longo de 33s. O delta tem janela de milissegundos, então o ruído não entra, e
	// mede exatamente o que interessa: se um descritor estivesse aberto, a remoção
	// devolveria ~0 em vez do tamanho do resíduo.
	livreAntesDeRemover := livreEmDisco(t, base)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	devolvido := livreEmDisco(t, base) - livreAntesDeRemover
	t.Logf("espaço devolvido pela remoção: %d MB (resíduo era %d MB)", devolvido>>20, parcial>>20)
	if devolvido < parcial*8/10 {
		t.Errorf("ELO 3 falhou: a remoção devolveu %d MB de %d MB — algum processo mantém o descritor aberto",
			devolvido>>20, parcial>>20)
	}
}

// fdsAbertosEm varre /proc/*/fd e devolve "pid -> caminho" para todo descritor que aponte
// para dentro de dir, inclusive os marcados "(deleted)" — que são justamente o caso em que
// o arquivo não aparece mais no ls mas os blocos seguem alocados.
func fdsAbertosEm(t *testing.T, dir string) []string {
	t.Helper()
	procs, err := os.ReadDir("/proc")
	if err != nil {
		t.Skip("/proc indisponível: não é possível verificar descritores abertos")
	}
	var donos []string
	for _, p := range procs {
		pid, err := strconv.Atoi(p.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		fds, err := os.ReadDir(filepath.Join("/proc", p.Name(), "fd"))
		if err != nil {
			continue // processo sumiu ou sem permissão (não é nosso)
		}
		for _, fd := range fds {
			destino, err := os.Readlink(filepath.Join("/proc", p.Name(), "fd", fd.Name()))
			if err == nil && strings.HasPrefix(destino, dir) {
				donos = append(donos, fmt.Sprintf("pid %d -> %s", pid, destino))
			}
		}
	}
	return donos
}

// pararQuandoCrescer espera a pasta passar de `limite` bytes e então manda SIGSTOP no grupo
// do yt-dlp — congelando a árvore inteira, como um stall de rede.
func pararQuandoCrescer(t *testing.T, dir string, limite int64) {
	t.Helper()
	go func() {
		fim := time.Now().Add(2 * time.Minute)
		for time.Now().Before(fim) {
			if tamanhoPasta(dir) >= limite {
				pids := pidsVivos(t, "yt-dlp")
				if len(pids) == 0 {
					time.Sleep(200 * time.Millisecond)
					continue
				}
				for _, p := range pids {
					if pgid, err := syscall.Getpgid(p); err == nil {
						syscall.Kill(-pgid, syscall.SIGSTOP) // congela a árvore
					} else {
						syscall.Kill(p, syscall.SIGSTOP)
					}
				}
				t.Logf("SIGSTOP enviado a %v com %d MB baixados", pids, tamanhoPasta(dir)>>20)
				return
			}
			time.Sleep(150 * time.Millisecond)
		}
		t.Log("a pasta não cresceu o suficiente para o SIGSTOP")
	}()
}

// pidsVivos lista PIDs cujo comando casa com o nome (pgrep -f), ignorando o próprio teste.
func pidsVivos(t *testing.T, nome string) []int {
	t.Helper()
	out, _ := exec.Command("pgrep", "-f", nome).Output()
	var pids []int
	for _, l := range strings.Fields(string(out)) {
		p, err := strconv.Atoi(l)
		if err != nil || p == os.Getpid() {
			continue
		}
		pids = append(pids, p)
	}
	return pids
}

func livreEmDisco(t *testing.T, dir string) int64 {
	t.Helper()
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		t.Fatal(fmt.Errorf("statfs %s: %w", dir, err))
	}
	return int64(st.Bavail) * int64(st.Bsize)
}
