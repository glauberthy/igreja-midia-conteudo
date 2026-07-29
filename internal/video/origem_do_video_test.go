package video

import (
	"context"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"srtclean/internal/pipeline"
	"srtclean/internal/validacao"
)

// Este arquivo guarda a ORIGEM DE TEMPO do video.mp4 (pipeline.Pedido.OrigemMs).
//
// O bug: o render supunha que o arquivo começava em ped.Inicio. Verdade para o vídeo baixado
// por janela (cmd/baixar), FALSO para o vídeo inteiro que o servidor baixa — cujo pedido.json
// tem um Inicio real (o início da pregação). Resultado: `cmd/render -id <pedido do servidor>`
// gerava Shorts deslocados pelo Inicio (49 min, no caso real), com a DURAÇÃO CORRETA.
//
// Por que o teste olha o CONTEÚDO, e não a duração: a duração sai certa nos dois casos. Foi
// exatamente ela que deixou o bug passar — quatro Shorts com 37/48/46/30 s, os números
// esperados, e a cena errada em todos. Um teste de duração aqui seria um teste que passa com
// o bug presente, coisa que este projeto já colecionou.
//
// A fonte sintética CODIFICA O TEMPO NA IMAGEM: o canal R de cada frame vale 2×T (T em
// segundos). Assim o frame do Short diz de que instante da fonte ele veio, e o teste compara
// um FATO da imagem, não um metadado.

const (
	fpsSintetico   = 5   // baixo de propósito: vídeo curto e rápido de gerar
	rPorSegundo    = 2   // canal R = rPorSegundo * T  (T em segundos)
	toleranciaCorR = 12  // folga para YUV 4:2:0 + x264 crf18 num campo de cor plana
	amostraX       = 540 // pixel de amostra: centro horizontal
	amostraY       = 200 // bem acima do gradiente do rodapé (que ocupa os últimos 520 px)
)

// TestCLIRenderizaPedidoDoServidorNaCenaCerta é o teste de regressão do bug de origem: um
// pedido criado pelo SERVIDOR (video.mp4 = vídeo inteiro, origem 0) renderizado pela CLI
// (Renderizar, que antes supunha ped.Inicio) tem de sair na cena certa.
//
// O pedido tem Inicio = 00:10:00 de propósito: é um Inicio REAL e diferente de zero, como o
// do servidor. Com a suposição antiga, o corte iria para 90 s − 600 s → 0 s (clampado), e o
// frame traria a cor de T=0 em vez da de T=90.
func TestCLIRenderizaPedidoDoServidorNaCenaCerta(t *testing.T) {
	exigirFfmpeg(t)
	base, out := t.TempDir(), t.TempDir()
	dir := filepath.Join(base, "p1")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	criarFonteSintetica(t, filepath.Join(dir, "video.mp4"), 120)

	// Pedido EXATAMENTE como o servidor grava: início real da pregação + origem declarada 0
	// (o arquivo é o vídeo inteiro). Vai para o disco e volta, porque é isso que o cmd/render
	// faz — se o campo não sobrevivesse ao JSON, o teste tinha de reprovar.
	ped := &pipeline.Pedido{ID: "p1", YouTubeURL: "https://x", Inicio: "00:10:00", Fim: "00:40:00"}
	ped.DeclararOrigem(0)
	if err := ped.Salvar(base); err != nil {
		t.Fatal(err)
	}
	lido, err := pipeline.Carregar(base, "p1")
	if err != nil {
		t.Fatal(err)
	}

	// Trecho em tempo ABSOLUTO, como a seleção produz: 00:01:30 a 00:01:40.
	cands := []validacao.Candidato{{Start: "00:01:30.000", End: "00:01:40.000", DurationSeconds: 10, Hook: "x"}}
	r := &Renderizador{Exec: ExecutorReal{}, Bin: "ffmpeg", BaseDir: base, OutDir: out, Preset: "ultrafast", CRF: "18"}
	paths, err := r.Renderizar(context.Background(), lido, cands)
	if err != nil {
		t.Fatalf("Renderizar: %v", err)
	}

	// (1) CONTEÚDO: o primeiro frame do Short tem de ser o instante 90 s da fonte.
	rGot := canalRdoPrimeiroFrame(t, paths[0])
	querCerto := rPorSegundo * 90
	queErrado := 0 // o que a suposição antiga daria: 90 s − 600 s, clampado em 0
	t.Logf("canal R medido no Short: %d (cena certa = %d, cena da suposição antiga = %d)",
		rGot, querCerto, queErrado)
	if abs(rGot-querCerto) > toleranciaCorR {
		msg := fmt.Sprintf("o Short saiu de outro instante da fonte: R=%d, esperado ~%d (T=90s)", rGot, querCerto)
		if abs(rGot-queErrado) <= toleranciaCorR {
			msg += " — e é exatamente a cena que a suposição ped.Inicio produzia: o render voltou a " +
				"deduzir a origem em vez de ler pipeline.Pedido.OrigemMs"
		}
		t.Error(msg)
	}

	// (2) E a DURAÇÃO está certa — nos dois casos. Está aqui para deixar registrado que ela
	// não distingue nada: se um dia alguém reintroduzir o bug, esta asserção continua verde.
	if d := duracaoSegundos(t, paths[0]); abs(int(d*1000)-10000) > 200 {
		t.Errorf("duração do Short = %.3fs, esperado 10s", d)
	}
}

// TestOrigemErradaProduziriaOutraCena é a contraprova: com a origem DECLARADA como ped.Inicio
// (o que a suposição antiga fazia), o mesmo pedido e o mesmo candidato dão outro frame. Sem
// isto, o teste acima poderia estar verde por acaso — por exemplo se a fonte sintética
// tivesse a mesma cor em todo instante, ou se o -ss não estivesse sendo aplicado.
func TestOrigemErradaProduziriaOutraCena(t *testing.T) {
	exigirFfmpeg(t)
	base, out := t.TempDir(), t.TempDir()
	dir := filepath.Join(base, "p1")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	criarFonteSintetica(t, filepath.Join(dir, "video.mp4"), 120)

	ped := &pipeline.Pedido{ID: "p1", YouTubeURL: "https://x", Inicio: "00:00:30", Fim: "00:02:00"}
	cands := []validacao.Candidato{{Start: "00:01:30.000", End: "00:01:40.000", DurationSeconds: 10, Hook: "x"}}
	// Duas pastas de saída: o nome do arquivo é o mesmo (short_01.mp4), então renderizar as
	// duas origens no mesmo OutDir faria a segunda sobrescrever a primeira — e o teste
	// compararia o mesmo arquivo com ele mesmo. (Aconteceu; é por isso que a comparação
	// "as duas origens dão frames diferentes" existe.)
	novoRend := func(sub string) *Renderizador {
		return &Renderizador{Exec: ExecutorReal{}, Bin: "ffmpeg", BaseDir: base,
			OutDir: filepath.Join(out, sub), Preset: "ultrafast", CRF: "18"}
	}

	// Origem 0 (a verdade para o vídeo inteiro) -> frame de T=90.
	certo, err := novoRend("origem0").RenderizarComOrigem(context.Background(), ped, cands, 0)
	if err != nil {
		t.Fatalf("render com origem 0: %v", err)
	}
	// Origem = ped.Inicio (30 s), a suposição antiga -> corte em 90−30 = 60 s -> frame de T=60.
	errado, err := novoRend("origem30s").RenderizarComOrigem(context.Background(), ped, cands, 30000)
	if err != nil {
		t.Fatalf("render com origem 30s: %v", err)
	}

	rCerto := canalRdoPrimeiroFrame(t, certo[0])
	rErrado := canalRdoPrimeiroFrame(t, errado[0])
	t.Logf("R com origem 0 = %d (T=90 → ~%d) | R com origem 30s = %d (T=60 → ~%d)",
		rCerto, rPorSegundo*90, rErrado, rPorSegundo*60)

	if abs(rCerto-rPorSegundo*90) > toleranciaCorR {
		t.Errorf("origem 0 devia dar o frame de T=90 (R~%d), deu R=%d", rPorSegundo*90, rCerto)
	}
	if abs(rErrado-rPorSegundo*60) > toleranciaCorR {
		t.Errorf("origem 30s devia dar o frame de T=60 (R~%d), deu R=%d", rPorSegundo*60, rErrado)
	}
	if abs(rCerto-rErrado) <= toleranciaCorR {
		t.Error("as duas origens produziram o MESMO frame: a fonte sintética não distingue instantes, " +
			"então o teste de cena certa não estaria provando nada")
	}
	// E as duas durações são iguais — a razão de o bug ter passado.
	dCerto, dErrado := duracaoSegundos(t, certo[0]), duracaoSegundos(t, errado[0])
	if abs(int((dCerto-dErrado)*1000)) > 200 {
		t.Errorf("durações diferentes (%.3f vs %.3f): o argumento do teste é que a duração NÃO "+
			"distingue as duas origens", dCerto, dErrado)
	}
}

// TestRenderizarSemOrigemDeclaradaFalhaClaro: pedido antigo (sem origem_ms) não renderiza com
// um padrão silencioso. Assumir foi como o bug nasceu.
func TestRenderizarSemOrigemDeclaradaFalhaClaro(t *testing.T) {
	base := t.TempDir()
	ped, cands := prepararPedido(t, base)
	ped.OrigemMs = nil // pedido gravado por uma versão anterior

	fx := &fakeExec{}
	r := &Renderizador{Exec: fx, Bin: "ffmpeg", BaseDir: base, OutDir: filepath.Join(base, "final")}
	_, err := r.Renderizar(context.Background(), ped, cands)
	if err == nil {
		t.Fatal("render sem origem declarada devia falhar, não escolher um padrão")
	}
	if len(fx.chamadas) != 0 {
		t.Errorf("o ffmpeg foi chamado %d vez(es) antes da falha: nenhum arquivo devia ser gerado", len(fx.chamadas))
	}
	// A mensagem tem de dizer o que falta e o que fazer — quem topar com ela é o operador.
	for _, quero := range []string{"origem_ms", "pedido.json", "vídeo inteiro"} {
		if !strings.Contains(err.Error(), quero) {
			t.Errorf("a mensagem não menciona %q: %v", quero, err)
		}
	}
	if ped.Status != pipeline.EstadoErro {
		t.Errorf("pedido devia ficar em erro, está %q", ped.Status)
	}
}

func exigirFfmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg não instalado: este teste compara o CONTEÚDO do frame, precisa do render real")
	}
}

// criarFonteSintetica gera um vídeo em que o canal R codifica o tempo (R = rPorSegundo*T).
// -g força keyframe a cada segundo: o render busca com -ss ANTES do -i (salto por índice), e
// sem keyframes frequentes o salto cairia num frame vizinho.
func criarFonteSintetica(t *testing.T, path string, segundos int) {
	t.Helper()
	cmd := exec.Command("ffmpeg", "-y", "-v", "error",
		"-f", "lavfi", "-i", fmt.Sprintf("color=c=black:s=192x108:r=%d:d=%d", fpsSintetico, segundos),
		"-vf", fmt.Sprintf("geq=r='%d*T':g='96':b='32'", rPorSegundo),
		"-c:v", "libx264", "-preset", "ultrafast", "-crf", "18",
		"-g", strconv.Itoa(fpsSintetico), "-pix_fmt", "yuv420p", path)
	if saida, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gerando fonte sintética: %v — %s", err, saida)
	}
}

// canalRdoPrimeiroFrame extrai o primeiro frame do Short e devolve o canal R de um pixel
// bem acima do gradiente do rodapé (que escureceria a amostra).
func canalRdoPrimeiroFrame(t *testing.T, videoPath string) int {
	t.Helper()
	png := videoPath + ".frame.png"
	cmd := exec.Command("ffmpeg", "-y", "-v", "error", "-i", videoPath,
		"-frames:v", "1", "-update", "1", png)
	if saida, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("extraindo frame de %s: %v — %s", videoPath, err, saida)
	}
	f, err := os.Open(png)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if b := m.Bounds(); b.Dx() != larguraSaida || b.Dy() != alturaSaida {
		t.Fatalf("frame com %dx%d, esperado %dx%d", b.Dx(), b.Dy(), larguraSaida, alturaSaida)
	}
	r, _, _, _ := m.At(amostraX, amostraY).RGBA()
	return int(r / 257) // 0..65535 -> 0..255
}

func duracaoSegundos(t *testing.T, videoPath string) float64 {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration",
		"-of", "csv=p=0", videoPath).Output()
	if err != nil {
		t.Fatalf("ffprobe em %s: %v", videoPath, err)
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		t.Fatalf("duração ilegível (%q): %v", out, err)
	}
	return d
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
