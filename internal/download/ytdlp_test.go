package download

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"srtclean/internal/pipeline"
)

// srtExemplo é uma legenda mínima que o "yt-dlp fake" grava no caminho de saída.
// Os tempos caem dentro da janela da pregação do pedidoTeste ([00:05:30, 00:38:10]),
// e há uma fala ANTES da janela (00:05:00) para exercitar o recorte inferior.
const srtExemplo = `1
00:05:00,000 --> 00:05:03,000
louvor antes da pregacao

2
00:05:31,000 --> 00:05:34,000
<i>A graça</i> de Deus é suficiente

3
00:05:35,000 --> 00:05:38,000
[Música]

4
00:05:39,000 --> 00:05:42,000
de verdade eu vos digo
`

// fakeExec simula o yt-dlp: registra as chamadas e delega a um handler por teste,
// que pode "criar" arquivos (legenda/vídeo) parseando o argumento -o.
type fakeExec struct {
	chamadas [][]string
	handler  func(dir string, args []string) (stderr []byte, err error)
}

func (f *fakeExec) Rodar(ctx context.Context, nome string, args ...string) ([]byte, []byte, error) {
	f.chamadas = append(f.chamadas, args)
	dir := dirDoOutput(args)
	if f.handler == nil {
		return nil, nil, nil
	}
	stderr, err := f.handler(dir, args)
	return nil, stderr, err
}

// dirDoOutput extrai a pasta a partir do argumento "-o <dir>/algo".
func dirDoOutput(args []string) string {
	for i, a := range args {
		if a == "-o" && i+1 < len(args) {
			return filepath.Dir(args[i+1])
		}
	}
	return ""
}

func ehLegenda(args []string) bool {
	for _, a := range args {
		if a == "--skip-download" {
			return true
		}
	}
	return false
}

func pedidoTeste(id string) *pipeline.Pedido {
	return pipeline.NovoPedido(id, "https://youtu.be/xyz", "00:05:30", "00:38:10", time.Unix(0, 0).UTC())
}

func TestBaixarSucesso(t *testing.T) {
	base := t.TempDir()
	fx := &fakeExec{handler: func(dir string, args []string) ([]byte, error) {
		if ehLegenda(args) {
			// yt-dlp gera legenda.pt.srt
			return nil, os.WriteFile(filepath.Join(dir, "legenda.pt.srt"), []byte(srtExemplo), 0644)
		}
		return nil, os.WriteFile(filepath.Join(dir, "video.mp4"), []byte("mp4"), 0644)
	}}
	b := &Baixador{Exec: fx, Bin: "yt-dlp", BaseDir: base}

	ped := pedidoTeste("teste")
	if err := b.Baixar(context.Background(), ped); err != nil {
		t.Fatalf("Baixar: %v", err)
	}

	dir := filepath.Join(base, "teste")
	for _, nome := range []string{"legenda.srt", "video.mp4", "transcricao.txt"} {
		if _, err := os.Stat(filepath.Join(dir, nome)); err != nil {
			t.Errorf("faltou o arquivo %s: %v", nome, err)
		}
	}

	// A transcrição deve ter passado pelo srtclean (sem tags, sem [Música]).
	tr, _ := os.ReadFile(filepath.Join(dir, "transcricao.txt"))
	txt := string(tr)
	if strings.Contains(txt, "<i>") || strings.Contains(txt, "[Música]") {
		t.Errorf("transcrição não foi limpa: %q", txt)
	}
	if !strings.Contains(txt, "[00:05:31] A graça de Deus é suficiente") {
		t.Errorf("transcrição inesperada: %q", txt)
	}
	// Recorte à janela [inicio, fim]: a fala de louvor antes de 00:05:30 não entra.
	if strings.Contains(txt, "louvor antes da pregacao") {
		t.Errorf("transcrição não foi recortada à janela da pregação: %q", txt)
	}
}

func TestBaixarSemLegenda(t *testing.T) {
	base := t.TempDir()
	// Handler não cria nenhum .srt (yt-dlp não encontrou legenda pt).
	fx := &fakeExec{handler: func(dir string, args []string) ([]byte, error) {
		return nil, nil
	}}
	b := &Baixador{Exec: fx, Bin: "yt-dlp", BaseDir: base}

	ped := pedidoTeste("semleg")
	err := b.Baixar(context.Background(), ped)

	if !errors.Is(err, ErrSemLegenda) {
		t.Fatalf("esperava ErrSemLegenda, veio: %v", err)
	}
	if ped.Status != pipeline.EstadoErro || ped.Erro == "" {
		t.Errorf("pedido devia ficar em erro com mensagem: status=%q erro=%q", ped.Status, ped.Erro)
	}
	// Fail-fast: não deve ter tentado baixar o vídeo (só 1 chamada, a da legenda).
	if len(fx.chamadas) != 1 {
		t.Errorf("esperava 1 chamada (só legenda), veio %d", len(fx.chamadas))
	}
}

func TestBaixarTempoInvalido(t *testing.T) {
	base := t.TempDir()
	fx := &fakeExec{}
	b := &Baixador{Exec: fx, Bin: "yt-dlp", BaseDir: base}

	casos := map[string][2]string{
		"fim antes do início": {"00:38:10", "00:05:30"},
		"iguais":              {"00:10:00", "00:10:00"},
		"formato ruim":        {"5m30", "38m10"},
		"vazio":               {"", ""},
	}
	for nome, tempos := range casos {
		ped := pipeline.NovoPedido("t", "url", tempos[0], tempos[1], time.Unix(0, 0).UTC())
		err := b.Baixar(context.Background(), ped)
		if !errors.Is(err, ErrTempoInvalido) {
			t.Errorf("%s: esperava ErrTempoInvalido, veio %v", nome, err)
		}
	}
	// yt-dlp nunca deve ter sido chamado.
	if len(fx.chamadas) != 0 {
		t.Errorf("yt-dlp não devia ser chamado com tempos inválidos; chamadas=%d", len(fx.chamadas))
	}
}

func TestBaixarVideoIndisponivel(t *testing.T) {
	base := t.TempDir()
	fx := &fakeExec{handler: func(dir string, args []string) ([]byte, error) {
		return []byte("ERROR: [youtube] xyz: Video unavailable"), errors.New("exit status 1")
	}}
	b := &Baixador{Exec: fx, Bin: "yt-dlp", BaseDir: base}

	ped := pedidoTeste("indisp")
	err := b.Baixar(context.Background(), ped)
	if !errors.Is(err, ErrVideoIndisponivel) {
		t.Fatalf("esperava ErrVideoIndisponivel, veio: %v", err)
	}
	if ped.Status != pipeline.EstadoErro {
		t.Errorf("pedido devia ficar em erro; status=%q", ped.Status)
	}
}

func TestArgsContemParametrosEssenciais(t *testing.T) {
	leg := argsLegenda("URL", "trabalho/x", "pt")
	if !contem(leg, "--skip-download") || !contem(leg, "--write-auto-subs") || !contem(leg, "pt") {
		t.Errorf("args de legenda incompletos: %v", leg)
	}
	if !contem(leg, "--force-overwrites") {
		t.Errorf("legenda deve usar --force-overwrites (não reaproveitar arquivo antigo): %v", leg)
	}
	vid := argsVideo("URL", "00:05:30", "00:38:10", "trabalho/x", "")
	if !contem(vid, "--download-sections") || !contem(vid, "*00:05:30-00:38:10") {
		t.Errorf("args de vídeo não recortam o intervalo: %v", vid)
	}
	// --force-overwrites impede o yt-dlp de pular um video.mp4 pré-existente (a causa
	// raiz de reaproveitar o vídeo de outro pedido).
	if !contem(vid, "--force-overwrites") {
		t.Errorf("vídeo deve usar --force-overwrites (não pular video.mp4 existente): %v", vid)
	}
	// Com formato vazio, não passa -f (deixa o yt-dlp escolher o melhor).
	if contem(vid, "-f") {
		t.Errorf("formato vazio não devia passar -f: %v", vid)
	}
	if contem(argsVideo("URL", "00:00:00", "00:01:00", "d", "18"), "-f") == false {
		t.Errorf("formato definido devia passar -f")
	}
}

func contem(xs []string, alvo string) bool {
	for _, x := range xs {
		if x == alvo {
			return true
		}
	}
	return false
}
