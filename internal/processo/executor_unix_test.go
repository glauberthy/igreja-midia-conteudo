//go:build unix

package processo

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// vivo diz se o processo existe. Sinal 0 não entrega nada: só testa a existência.
func vivo(pid int) bool { return syscall.Kill(pid, 0) == nil }

func esperarMorrer(t *testing.T, pid int, limite time.Duration) bool {
	t.Helper()
	fim := time.Now().Add(limite)
	for time.Now().Before(fim) {
		if !vivo(pid) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return !vivo(pid)
}

// TestCancelamentoMataNeto é o ponto central: o yt-dlp cria um ffmpeg filho. Matar só o
// processo direto deixa o neto vivo, e um neto vivo com o arquivo aberto impede o Linux de
// devolver o espaço mesmo depois do unlink — o log diria "liberei 900 MB" e o df
// continuaria cheio.
func TestCancelamentoMataNeto(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "neto.pid")
	alvo := filepath.Join(dir, "saida.bin")

	// O "pai" cria um neto que escreve sem parar, publica o PID dele e espera.
	script := `( while true; do printf 0123456789 >> ` + alvo + `; done ) & echo $! > ` + pidFile + `; wait`

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	inicio := time.Now()
	_, _, err := Rodar(ctx, "sh", "-c", script)
	decorrido := time.Since(inicio)

	if err == nil {
		t.Fatal("esperava erro de cancelamento")
	}
	// Se Wait() travasse esperando o pipe do neto, isto passaria de EsperaAposMatar.
	if decorrido > EsperaAposMatar+2*time.Second {
		t.Errorf("Rodar demorou %s a retornar — Wait() ficou preso no pipe de um neto vivo", decorrido)
	}

	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Skip("o shell não publicou o PID do neto a tempo; nada a verificar")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		t.Skipf("PID do neto ilegível: %q", b)
	}
	if !esperarMorrer(t, pid, 3*time.Second) {
		syscall.Kill(pid, syscall.SIGKILL) // não deixa lixo para a próxima execução
		t.Fatalf("o neto (pid %d) sobreviveu ao cancelamento — o grupo de processos não foi morto", pid)
	}
}

// livreNoSistema devolve os bytes livres do filesystem que contém dir.
func livreNoSistema(t *testing.T, dir string) int64 {
	t.Helper()
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		t.Skipf("statfs indisponível: %v", err)
	}
	return int64(st.Bavail) * int64(st.Bsize)
}

// TestEspacoVoltaAposCancelamento é o teste que o dono pediu, e ele mede o que importa:
// espaço livre do FILESYSTEM (statfs), não presença do arquivo.
//
// A distinção é o bug inteiro. No Linux, apagar um arquivo que um processo mantém aberto
// remove o nome mas NÃO devolve os blocos — só quando o descritor fecha. Então "o arquivo
// sumiu" passa mesmo com o neto vivo; só o df mostra a verdade. É por isso que o neto aqui
// segura o descritor (exec 3>) enquanto dorme, em vez de só escrever e sair.
func TestEspacoVoltaAposCancelamento(t *testing.T) {
	dir := t.TempDir()
	alvo := filepath.Join(dir, "grande.bin")
	const mb = 60

	if livreNoSistema(t, dir) < 500<<20 {
		t.Skip("disco muito cheio para medir com confiança")
	}

	// O neto abre o arquivo no fd 3, escreve 60 MB e DORME segurando o descritor.
	script := `( exec 3>` + alvo + `; dd if=/dev/zero bs=1M count=` + strconv.Itoa(mb) +
		` >&3 2>/dev/null; sleep 60 ) & wait`

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	Rodar(ctx, "sh", "-c", script)

	info, err := os.Stat(alvo)
	if err != nil || info.Size() < mb<<20 {
		t.Skip("o neto não chegou a escrever os 60 MB; nada a medir")
	}

	// A prova: os blocos voltaram ao filesystem? Com o neto vivo, não voltariam.
	//
	// Medido como DELTA da remoção, não contra o livre do início do teste: numa máquina em
	// uso, o valor absoluto capta a atividade de todo o resto (no experimento real isso
	// acusou "75 MB presos" com resíduo de 9 MB). A janela do delta é de milissegundos.
	livreAntesDeRemover := livreNoSistema(t, dir)
	if err := os.Remove(alvo); err != nil {
		t.Fatalf("removendo o resíduo: %v", err)
	}
	devolvido := livreNoSistema(t, dir) - livreAntesDeRemover
	if devolvido < (mb<<20)*8/10 {
		t.Fatalf("a remoção devolveu %d MB de %d MB — um processo sobrevivente mantém o descritor aberto",
			devolvido>>20, mb)
	}
}

// TestComandoNormalNaoEhAfetado: o isolamento em grupo não pode mudar o caminho feliz.
func TestComandoNormalNaoEhAfetado(t *testing.T) {
	out, _, err := Rodar(context.Background(), "sh", "-c", "printf ola")
	if err != nil {
		t.Fatalf("comando simples falhou: %v", err)
	}
	if string(out) != "ola" {
		t.Errorf("stdout perdido: %q", out)
	}
}

// TestStderrPreservadoNoErro: a mensagem do yt-dlp/ffmpeg é o que explica a falha ao
// operador; ela não pode se perder no caminho novo.
func TestStderrPreservadoNoErro(t *testing.T) {
	_, errb, err := Rodar(context.Background(), "sh", "-c", "echo problema >&2; exit 3")
	if err == nil {
		t.Fatal("esperava erro de saída 3")
	}
	if !strings.Contains(string(errb), "problema") {
		t.Errorf("stderr perdido: %q", errb)
	}
}
