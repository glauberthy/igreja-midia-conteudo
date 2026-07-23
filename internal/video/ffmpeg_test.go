package video

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"srtclean/internal/pipeline"
	"srtclean/internal/validacao"
)

// Transcrição limpa ([HH:MM:SS] texto), a mesma fonte que a legenda usa (spec-12).
// Cobre a janela dos candidatos de prepararPedido (~01:30:10 a 01:31:30).
const transcricaoFixture = `[01:30:10] Deus nos criou para viver em comunhão com ele todos os dias.
[01:30:22] Ele nunca abandona quem confia de coração no seu cuidado.
[01:30:40] A graça de Cristo alcança o pecador perdido e oferece vida nova.
[01:31:00] O amor de Deus transforma por dentro o coração humano.
[01:31:16] Por isso descansa e confia no Senhor em toda a tua jornada.
`

func TestArgsFFmpegSemLogoUsaVf(t *testing.T) {
	args := ArgsFFmpeg("trabalho/x/video.mp4", "", "finalizados/x/short_01.mp4", 65000, 30000, "crop=ih*9/16:ih,scale=1080:1920", false)
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "-ss 65.000") {
		t.Errorf("corte não começa em start-inicio: %s", joined)
	}
	if !strings.Contains(joined, "-t 30.000") {
		t.Errorf("duração do corte ausente: %s", joined)
	}
	if !strings.Contains(joined, "-vf crop=ih*9/16:ih,scale=1080:1920") {
		t.Errorf("sem logo/gradiente deveria usar -vf: %s", joined)
	}
	if strings.Contains(joined, "filter_complex") {
		t.Errorf("modo simples não deveria usar filter_complex: %s", joined)
	}
}

func TestArgsFFmpegComplexoComLogo(t *testing.T) {
	args := ArgsFFmpeg("trabalho/x/video.mp4", "assets/ibi_assinatura_shorts.png", "out.mp4", 65000, 30000, "[0:v]crop[v0];[v0][logo]overlay[vout]", true)
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "-i assets/ibi_assinatura_shorts.png") {
		t.Errorf("logo não entrou como 2º input: %s", joined)
	}
	if !strings.Contains(joined, "-filter_complex") || !strings.Contains(joined, "[vout]") {
		t.Errorf("deveria usar filter_complex com saída [vout]: %s", joined)
	}
	if !strings.Contains(joined, "-map [vout]") || !strings.Contains(joined, "-map 0:a?") {
		t.Errorf("mapeamento de vídeo/áudio ausente: %s", joined)
	}
}

func TestArgsFFmpegComplexoSemLogo(t *testing.T) {
	// Gradiente sem logo: filter_complex mas SEM 2º input.
	args := ArgsFFmpeg("trabalho/x/video.mp4", "", "out.mp4", 0, 30000, "[0:v]crop[v0];[v0][grad]overlay[vout]", true)
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "-i out.mp4") || strings.Count(joined, "-i ") != 1 {
		t.Errorf("sem logo não deveria ter 2º input: %s", joined)
	}
	if !strings.Contains(joined, "-filter_complex") || !strings.Contains(joined, "-map [vout]") {
		t.Errorf("deveria usar filter_complex com map [vout]: %s", joined)
	}
}

func estiloTeste() EstiloLegenda {
	return EstiloLegenda{FontePath: "assets/fontes/static/GoogleSansFlex_72pt-Bold.ttf", Tamanho: 54, Contorno: 4, Sombra: 2, EspacoLinhas: 10, FaixaLogoPx: 240}
}

func TestMontarFiltroSimplesSemLogoNemGradiente(t *testing.T) {
	blocos := []BlocoLegenda{{InicioMs: 0, FimMs: 2000, Texto: "olá\nmundo"}}
	tfs := []string{"trabalho/x/short_01.sub001.txt"}
	f, complexo := montarFiltro(blocos, tfs, estiloTeste(), false, LogoConfig{}, GradConfig{})
	if complexo {
		t.Error("sem logo nem gradiente deveria ser -vf simples")
	}
	if !strings.HasPrefix(f, "crop=ih*9/16:ih,scale=1080:1920,setsar=1,drawtext=") {
		t.Errorf("cadeia -vf inesperada: %s", f)
	}
	if !strings.Contains(f, "y=h-240-text_h") || !strings.Contains(f, "text_align=C") {
		t.Errorf("legenda não ancorada na base/centralizada: %s", f)
	}
}

// spec-12 (bug legenda duplicada): a janela do drawtext usa limite superior EXCLUSIVO,
// para dois blocos vizinhos (que compartilham a fronteira) nunca ficarem ativos juntos.
func TestDrawtextFiltrosJanelaSemiaberta(t *testing.T) {
	blocos := []BlocoLegenda{
		{InicioMs: 0, FimMs: 5000, Texto: "um"},
		{InicioMs: 5000, FimMs: 7000, Texto: "dois"},
	}
	tfs := []string{"a.txt", "b.txt"}
	f := drawtextFiltros(blocos, tfs, estiloTeste())

	if strings.Contains(f, "between(") {
		t.Errorf("não deveria usar between (inclusivo nos dois extremos): %s", f)
	}
	// bloco 0: [0, 5000)  -> gte(t,0.000)*lt(t,5.000)
	if !strings.Contains(f, "enable='gte(t,0.000)*lt(t,5.000)'") {
		t.Errorf("bloco 0 sem janela semiaberta: %s", f)
	}
	// bloco 1: [5000, 7000) -> gte(t,5.000)*lt(t,7.000)
	if !strings.Contains(f, "enable='gte(t,5.000)*lt(t,7.000)'") {
		t.Errorf("bloco 1 sem janela semiaberta: %s", f)
	}
	// Na fronteira t=5.000: bloco0 lt(5.000)=falso, bloco1 gte(5.000)=verdadeiro → só um.
}

func TestMontarFiltroComGradienteELogo(t *testing.T) {
	blocos := []BlocoLegenda{{InicioMs: 0, FimMs: 2000, Texto: "primeira\nsegunda"}}
	tfs := []string{"trabalho/x/short_01.sub001.txt"}
	logo := LogoConfig{Path: "assets/ibi_assinatura_shorts.png", LarguraPx: 560, AjusteY: 0}
	grad := GradConfig{Altura: 720, Alpha: 0.90}

	f, complexo := montarFiltro(blocos, tfs, estiloTeste(), true, logo, grad)
	if !complexo {
		t.Fatal("com logo/gradiente deveria ser filter_complex")
	}
	// gradiente do rodapé (preto com alpha em rampa suave), overlay na base
	if !strings.Contains(f, "color=c=black:s=1080x720") || !strings.Contains(f, "geq=r=0:g=0:b=0:a='0.90*255*pow(Y/H") {
		t.Errorf("gradiente do rodapé ausente/errado: %s", f)
	}
	if !strings.Contains(f, "[grad]overlay=0:H-h[vg]") {
		t.Errorf("gradiente não sobreposto na base: %s", f)
	}
	// legenda sobre o gradiente
	if !strings.Contains(f, "[vg]drawtext=") {
		t.Errorf("legenda não desenhada sobre o gradiente: %s", f)
	}
	// logo por cima de tudo, centralizada e ancorada na base, saída [vout]
	if !strings.Contains(f, "[1:v]scale=560:-2[logo]") {
		t.Errorf("logo não escalada: %s", f)
	}
	// logo centralizada na faixa (H - faixa/2 - h/2), ajuste 0
	if !strings.Contains(f, "overlay=x=(W-w)/2:y=H-240/2-h/2+0[vout]") {
		t.Errorf("logo não centralizada na faixa com saída [vout]: %s", f)
	}
}

func TestMontarFiltroGradienteSemLogoFechaEmVout(t *testing.T) {
	// Só gradiente (sem logo): ainda filter_complex e a saída precisa ser [vout].
	f, complexo := montarFiltro(nil, nil, estiloTeste(), false, LogoConfig{}, GradConfig{Altura: 720, Alpha: 0.9})
	if !complexo {
		t.Fatal("gradiente ativo deveria exigir filter_complex")
	}
	if !strings.HasSuffix(f, "[vout]") {
		t.Errorf("filter_complex deve terminar em [vout]: %s", f)
	}
	if strings.Contains(f, "[1:v]") {
		t.Errorf("sem logo não deveria referenciar [1:v]: %s", f)
	}
}

// fakeExec simula o ffmpeg: registra chamadas e cria o arquivo de saída.
type fakeExec struct {
	chamadas [][]string
	falhar   bool
}

func (f *fakeExec) Rodar(ctx context.Context, nome string, args ...string) ([]byte, []byte, error) {
	f.chamadas = append(f.chamadas, args)
	if f.falhar {
		return nil, []byte("ffmpeg: Invalid data found"), errors.New("exit status 1")
	}
	// Cria o arquivo de saída (último argumento) para simular sucesso.
	out := args[len(args)-1]
	os.WriteFile(out, []byte("mp4 fake"), 0644)
	return nil, nil, nil
}

func TestDuracaoComMargem(t *testing.T) {
	// Sem margem: duração = end - start (corte no end original).
	if d, err := duracaoComMargem(0, 34000, 0); err != nil || d != 34000 {
		t.Errorf("sem margem: d=%d err=%v; queria 34000,nil", d, err)
	}
	// Com margem default (400 ms): recua o fim → 33600 ms.
	if d, err := duracaoComMargem(0, 34000, 400); err != nil || d != 33600 {
		t.Errorf("margem 400ms: d=%d err=%v; queria 33600,nil", d, err)
	}
	// A margem apara exatamente `margem` do fim, independentemente do start absoluto.
	if d, err := duracaoComMargem(90000, 124000, 400); err != nil || d != 33600 {
		t.Errorf("start deslocado: d=%d err=%v; queria 33600,nil", d, err)
	}
}

func TestDuracaoComMargemGuard(t *testing.T) {
	// Guard: margem >= duração inverteria/zeraria o trecho → erro, não corte.
	if _, err := duracaoComMargem(0, 400, 400); err == nil {
		t.Error("margem igual à duração deveria dar erro (recuo zeraria o trecho)")
	}
	if _, err := duracaoComMargem(0, 300, 400); err == nil {
		t.Error("margem maior que a duração deveria dar erro (recuo inverteria o trecho)")
	}
	// Trecho já vazio/invertido também é erro.
	if _, err := duracaoComMargem(1000, 1000, 0); err == nil {
		t.Error("trecho de duração zero deveria dar erro")
	}
}

func prepararPedido(t *testing.T, base string) (*pipeline.Pedido, []validacao.Candidato) {
	t.Helper()
	id := "teste"
	dir := filepath.Join(base, id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "video.mp4"), []byte("v"), 0644); err != nil {
		t.Fatal(err)
	}
	// Legenda vem do texto LIMPO da transcrição (spec-12), não mais do SRT bruto.
	if err := os.WriteFile(filepath.Join(dir, "transcricao.txt"), []byte(transcricaoFixture), 0644); err != nil {
		t.Fatal(err)
	}
	ped := pipeline.NovoPedido(id, "url", "01:29:38", "02:05:11", time.Unix(0, 0).UTC())
	// Dois candidatos com scores diferentes, para checar a ordenação. Vêm de fora do
	// pedido (spec-09): o render os recebe do arquivo validado, não do pedido.json.
	cands := []validacao.Candidato{
		{Start: "01:31:00.000", End: "01:31:30.000", Score: 85, Hook: "menor score"},
		{Start: "01:30:10.000", End: "01:30:40.000", Score: 97, Hook: "maior score"},
	}
	return ped, cands
}

func TestRenderizarGeraPorScore(t *testing.T) {
	base := t.TempDir()
	outBase := filepath.Join(base, "final")
	ped, cands := prepararPedido(t, base)

	fx := &fakeExec{}
	r := &Renderizador{Exec: fx, Bin: "ffmpeg", BaseDir: base, OutDir: outBase}

	paths, err := r.Renderizar(context.Background(), ped, cands)
	if err != nil {
		t.Fatalf("Renderizar: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("esperava 2 Shorts, veio %d", len(paths))
	}

	// short_01 = maior score; deve cortar em 01:30:10 - 01:29:38 = 32s.
	if !strings.HasSuffix(paths[0], filepath.Join(outBase, "teste", "short_01.mp4")) {
		t.Errorf("short_01 com caminho inesperado: %s", paths[0])
	}
	joined01 := strings.Join(fx.chamadas[0], " ")
	if !strings.Contains(joined01, "-ss 32.000") {
		t.Errorf("short_01 (maior score) devia cortar em 32s (start-inicio): %s", joined01)
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("arquivo de saída não criado: %s", p)
		}
	}
}

func TestRenderizarAplicaMargemNoCorte(t *testing.T) {
	base := t.TempDir()
	ped, cands := prepararPedido(t, base) // candidatos de 30 s (01:30:10→01:30:40 etc.)

	fx := &fakeExec{}
	// Margem de 400 ms: o -t (duração do corte) deve virar 30s - 0,4s = 29.600.
	r := &Renderizador{Exec: fx, Bin: "ffmpeg", BaseDir: base, OutDir: filepath.Join(base, "final"), MargemFimMs: 400}

	if _, err := r.Renderizar(context.Background(), ped, cands); err != nil {
		t.Fatalf("Renderizar: %v", err)
	}
	joined := strings.Join(fx.chamadas[0], " ")
	if !strings.Contains(joined, "-t 29.600") {
		t.Errorf("esperava corte de 29.600s (30s - margem 0,4s): %s", joined)
	}
	if strings.Contains(joined, "-t 30.000") {
		t.Errorf("margem não foi aplicada (ainda 30.000s): %s", joined)
	}
}

func TestRenderizarErroFfmpeg(t *testing.T) {
	base := t.TempDir()
	ped, cands := prepararPedido(t, base)
	r := &Renderizador{Exec: &fakeExec{falhar: true}, Bin: "ffmpeg", BaseDir: base, OutDir: filepath.Join(base, "final")}

	_, err := r.Renderizar(context.Background(), ped, cands)
	if err == nil {
		t.Fatal("esperava erro do ffmpeg")
	}
	if ped.Status != pipeline.EstadoErro || ped.Erro == "" {
		t.Errorf("pedido devia ficar em erro com mensagem: status=%q erro=%q", ped.Status, ped.Erro)
	}
}

func TestRenderizarSemCandidatos(t *testing.T) {
	base := t.TempDir()
	ped, _ := prepararPedido(t, base)
	r := &Renderizador{Exec: &fakeExec{}, Bin: "ffmpeg", BaseDir: base, OutDir: filepath.Join(base, "final")}
	if _, err := r.Renderizar(context.Background(), ped, nil); err == nil {
		t.Error("esperava erro para render sem candidatos")
	}
}
