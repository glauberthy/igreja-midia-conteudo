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

const legendaSRT = `1
00:01:30,000 --> 00:01:33,000
<i>A graça</i> de Deus

2
00:01:33,000 --> 00:01:36,000
é suficiente para você

3
00:01:36,000 --> 00:01:39,000
[Música]

4
00:01:39,000 --> 00:01:42,000
de verdade eu vos digo
`

func TestParseSRT(t *testing.T) {
	cues := ParseSRT(legendaSRT)
	// O bloco [Música] vira vazio e é descartado.
	if len(cues) != 3 {
		t.Fatalf("esperava 3 cues, veio %d: %+v", len(cues), cues)
	}
	if cues[0].InicioMs != 90000 || cues[0].FimMs != 93000 {
		t.Errorf("tempos do cue 0 inesperados: %+v", cues[0])
	}
	if cues[0].Texto != "A graça de Deus" { // tag <i> removida, palavras intactas
		t.Errorf("texto do cue 0 inesperado: %q", cues[0].Texto)
	}
}

func TestRecortarLegendaRebaseiaAzero(t *testing.T) {
	cues := ParseSRT(legendaSRT)
	// Trecho [00:01:32, 00:01:40] (92s a 100s).
	rec := RecortarLegenda(cues, 92000, 100000)

	if len(rec) != 3 {
		t.Fatalf("esperava 3 cues no trecho, veio %d: %+v", len(rec), rec)
	}
	// O primeiro cue começava em 90s mas o trecho começa em 92s: recorta e rebaseia a 0.
	if rec[0].InicioMs != 0 {
		t.Errorf("cue 0 não foi rebaseado a zero: %d", rec[0].InicioMs)
	}
	if rec[0].FimMs != 1000 { // 93s - 92s
		t.Errorf("cue 0 fim inesperado: %d", rec[0].FimMs)
	}
	// O último cue (39s..42s abs = 129s..132s? não) -> 00:01:39 = 99000..102000, trecho até 100000:
	last := rec[len(rec)-1]
	if last.InicioMs != 7000 { // 99000 - 92000
		t.Errorf("último cue início inesperado: %d", last.InicioMs)
	}
	if last.FimMs != 8000 { // clamp em 100000 -> 100000-92000
		t.Errorf("último cue fim não foi cortado na borda: %d", last.FimMs)
	}
}

func TestRecortarLegendaSemSobreposicao(t *testing.T) {
	cues := ParseSRT(legendaSRT)
	if rec := RecortarLegenda(cues, 200000, 210000); len(rec) != 0 {
		t.Errorf("esperava 0 cues fora do intervalo, veio %d", len(rec))
	}
}

func TestFormatarSRT(t *testing.T) {
	s := FormatarSRT([]Cue{{InicioMs: 0, FimMs: 1500, Texto: "olá"}})
	if !strings.Contains(s, "00:00:00,000 --> 00:00:01,500") || !strings.Contains(s, "olá") {
		t.Errorf("SRT formatado inesperado:\n%s", s)
	}
}

func TestArgsFFmpeg9x16(t *testing.T) {
	args := ArgsFFmpeg("trabalho/x/video.mp4", "trabalho/x/short_01.srt", "finalizados/x/short_01.mp4", 65000, 30000)
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "-ss 65.000") {
		t.Errorf("corte não começa em start-inicio: %s", joined)
	}
	if !strings.Contains(joined, "-t 30.000") {
		t.Errorf("duração do corte ausente: %s", joined)
	}
	if !strings.Contains(joined, "crop=ih*9/16:ih") || !strings.Contains(joined, "scale=1080:1920") {
		t.Errorf("reenquadramento 9:16 ausente: %s", joined)
	}
	if !strings.Contains(joined, "subtitles=") {
		t.Errorf("queima de legenda ausente: %s", joined)
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
	if err := os.WriteFile(filepath.Join(dir, "legenda.srt"), []byte(legendaSRT), 0644); err != nil {
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
