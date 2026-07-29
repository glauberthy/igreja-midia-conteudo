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
			// yt-dlp gera legenda.pt.srt + o .info.json (com o título) do --write-info-json.
			if err := os.WriteFile(filepath.Join(dir, "legenda.info.json"), []byte(`{"title":"Culto de Domingo — 19/07"}`), 0644); err != nil {
				return nil, err
			}
			return nil, os.WriteFile(filepath.Join(dir, "legenda.pt.srt"), []byte(srtExemplo), 0644)
		}
		return nil, os.WriteFile(filepath.Join(dir, "video.mp4"), []byte("mp4"), 0644)
	}}
	b := &Baixador{Exec: fx, Bin: "yt-dlp", BaseDir: base}

	ped := pedidoTeste("teste")
	origemMs, err := b.Baixar(context.Background(), ped)
	if err != nil {
		t.Fatalf("Baixar: %v", err)
	}

	// O título deve ter sido extraído do .info.json.
	if ped.Titulo != "Culto de Domingo — 19/07" {
		t.Errorf("título não extraído do info.json: %q", ped.Titulo)
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

	// QUEM ESCREVE O VÍDEO DIZ ONDE ELE COMEÇA — DEVOLVENDO, não escrevendo no Pedido. Aqui o
	// download é por JANELA (--download-sections), então o arquivo começa em t=0 no início da
	// janela: 00:05:30 = 330000 ms.
	if origemMs != 330000 {
		t.Errorf("origem devolvida = %d ms, quero 330000 (00:05:30, o início da janela baixada)", origemMs)
	}
	// E o Baixador NÃO mexe no Pedido: guardar é decisão de quem chama (o cmd/baixar declara;
	// o servidor recebe e persiste). Se voltar a escrever aqui, a escrita se perde em silêncio
	// no caminho do servidor, que passa uma CÓPIA do pedido — foi exatamente assim que a
	// origem virou bug. Ver spec-09.
	if ped.OrigemMs != nil {
		t.Errorf("o Baixador escreveu a origem no Pedido (%d): ele deve DEVOLVER, não mutar — "+
			"mutação através de cópia se perde sem deixar rastro", *ped.OrigemMs)
	}
}

// TestBaixarVideoCompletoDevolveOrigemZero: o outro caminho, o do servidor. O arquivo é o
// vídeo INTEIRO, então a origem é 0 — e NÃO ped.Inicio, que aqui é o início da pregação
// (00:05:30). Cada caminho devolve o que de fato produziu.
func TestBaixarVideoCompletoDevolveOrigemZero(t *testing.T) {
	base := t.TempDir()
	fx := &fakeExec{handler: func(dir string, args []string) ([]byte, error) {
		return nil, os.WriteFile(filepath.Join(dir, "video.mp4"), []byte("mp4"), 0644)
	}}
	b := &Baixador{Exec: fx, Bin: "yt-dlp", BaseDir: base}

	ped := pedidoTeste("inteiro")
	origemMs, err := b.BaixarVideoCompleto(context.Background(), ped, filepath.Join(base, "cache", "inteiro"))
	if err != nil {
		t.Fatalf("BaixarVideoCompleto: %v", err)
	}
	if origemMs != 0 {
		t.Errorf("origem devolvida = %d ms, quero 0 (o arquivo é o vídeo inteiro)", origemMs)
	}
	if ped.OrigemMs != nil {
		t.Errorf("o Baixador escreveu a origem no Pedido (%d): aqui ele recebe uma CÓPIA no "+
			"servidor, e a escrita se perderia sem rastro", *ped.OrigemMs)
	}
	// A armadilha, explícita: Inicio continua sendo o início da PREGAÇÃO, não a origem.
	if ped.Inicio != "00:05:30" {
		t.Errorf("Inicio não devia ter sido alterado para servir de origem: %q", ped.Inicio)
	}
}

// TestBaixarFalhoDevolveErro: quem chama só declara a origem quando err == nil, então o
// contrato que o download tem de cumprir é ERRAR de forma inequívoca. Um pedido cujo download
// falhou não tem vídeo, e declarar origem de um arquivo inexistente faria o render prosseguir
// sobre lixo em vez de recusar.
func TestBaixarFalhoDevolveErro(t *testing.T) {
	base := t.TempDir()
	fx := &fakeExec{handler: func(dir string, args []string) ([]byte, error) {
		if ehLegenda(args) {
			os.WriteFile(filepath.Join(dir, "legenda.info.json"), []byte(`{"title":"t"}`), 0644)
			return nil, os.WriteFile(filepath.Join(dir, "legenda.pt.srt"), []byte(srtExemplo), 0644)
		}
		return []byte("ERROR: Video unavailable"), errors.New("exit status 1")
	}}
	b := &Baixador{Exec: fx, Bin: "yt-dlp", BaseDir: base}

	ped := pedidoTeste("falhou")
	origemMs, err := b.Baixar(context.Background(), ped)
	if err == nil {
		t.Fatal("esperava erro no download do vídeo")
	}
	if origemMs != 0 {
		t.Errorf("origem devolvida = %d num download que falhou: devolva o zero-value, "+
			"o valor não tem significado sem arquivo", origemMs)
	}
	if ped.OrigemMs != nil {
		t.Errorf("origem escrita no Pedido (%d) num download que falhou: não há vídeo para descrever",
			*ped.OrigemMs)
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
	_, err := b.Baixar(context.Background(), ped)

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
		_, err := b.Baixar(context.Background(), ped)
		if !errors.Is(err, ErrTempoInvalido) {
			t.Errorf("%s: esperava ErrTempoInvalido, veio %v", nome, err)
		}
	}
	// yt-dlp nunca deve ter sido chamado.
	if len(fx.chamadas) != 0 {
		t.Errorf("yt-dlp não devia ser chamado com tempos inválidos; chamadas=%d", len(fx.chamadas))
	}
}

// A fase pesada baixa o vídeo INTEIRO com o downloader NATIVO em paralelo — é o que
// destrava a velocidade (~79x vs a janela contígua; ver spec-05). Este teste fixa as
// escolhas que dão essa velocidade, para não regredirem sem querer.
func TestArgsVideoCompletoUsaDownloaderNativoParalelo(t *testing.T) {
	args := argsVideoCompleto("URL", "trabalho/x", FormatoPadrao)

	if !contem(args, "--concurrent-fragments") || !contem(args, "8") {
		t.Errorf("faltou o paralelismo de fragmentos (o que dá a velocidade): %v", args)
	}
	// NÃO pode voltar a entregar o download ao ffmpeg: é o caminho lento (1 conexão).
	if contem(args, "--download-sections") {
		t.Errorf("--download-sections entrega o download ao ffmpeg (lento, 1 conexão): %v", args)
	}
	if contem(args, "--downloader-args") {
		t.Errorf("--downloader-args (ffmpeg) não se aplica ao downloader nativo: %v", args)
	}
	if !contem(args, "--force-overwrites") {
		t.Errorf("sem --force-overwrites o yt-dlp reaproveita o vídeo de outro pedido: %v", args)
	}
}

// O teto de formato é 1080, NÃO 720: hoje a transmissão é 720p (e é isso que vem), mas
// quando subir para 1080p o pipeline aproveita sozinho. O teto evita baixar 4K à toa.
func TestFormatoPadraoTeto1080(t *testing.T) {
	if !strings.Contains(FormatoPadrao, "height<=1080") {
		t.Errorf("FormatoPadrao deveria limitar a 1080: %q", FormatoPadrao)
	}
	if strings.Contains(FormatoPadrao, "height<=720") {
		t.Errorf("NÃO fixe em 720 — quando a igreja subir para 1080p o pipeline deve aproveitar sozinho: %q", FormatoPadrao)
	}
	// Sem formato explícito, o Baixador usa o padrão (nunca "melhor disponível", que pegaria 4K).
	b := &Baixador{}
	if b.formato() != FormatoPadrao {
		t.Errorf("formato() sem configuração = %q, quero o padrão", b.formato())
	}
}

// --- Anti-bot / 429: erro nomeado + retry com espera crescente ---

// capturaRetryDownload troca o log e o sleep durante o teste (não espera de verdade) e
// devolve os logs e as esperas praticadas.
func capturaRetryDownload(t *testing.T) (*[]string, *[]time.Duration) {
	t.Helper()
	var logs []string
	var esperas []time.Duration
	logOrig, dormirOrig := LogTentativaDownload, dormir
	LogTentativaDownload = func(m string) { logs = append(logs, m) }
	dormir = func(d time.Duration) { esperas = append(esperas, d) }
	t.Cleanup(func() { LogTentativaDownload, dormir = logOrig, dormirOrig })
	return &logs, &esperas
}

func TestBaixarAntiBotRefazComEsperaCrescente(t *testing.T) {
	_, esperas := capturaRetryDownload(t)
	base := t.TempDir()
	n := 0
	fx := &fakeExec{handler: func(dir string, args []string) ([]byte, error) {
		if ehLegenda(args) {
			n++
			if n < 3 { // 2 primeiras: anti-bot; 3ª: sucesso
				return []byte("ERROR: [youtube] xyz: Sign in to confirm you’re not a bot."), errors.New("exit status 1")
			}
			return nil, os.WriteFile(filepath.Join(dir, "legenda.pt.srt"), []byte(srtExemplo), 0644)
		}
		return nil, os.WriteFile(filepath.Join(dir, "video.mp4"), []byte("mp4"), 0644)
	}}
	b := &Baixador{Exec: fx, Bin: "yt-dlp", BaseDir: base}

	if _, err := b.Baixar(context.Background(), pedidoTeste("antibot")); err != nil {
		t.Fatalf("deveria suceder na 3ª tentativa: %v", err)
	}
	// Espera CRESCENTE entre as tentativas (30s, 60s) — não insiste rápido demais.
	if len(*esperas) != 2 || (*esperas)[0] != 30*time.Second || (*esperas)[1] != 60*time.Second {
		t.Errorf("esperas = %v, quero [30s 60s] (crescente)", *esperas)
	}
}

func TestBaixarAntiBotEsgotaComErroNomeado(t *testing.T) {
	capturaRetryDownload(t)
	base := t.TempDir()
	fx := &fakeExec{handler: func(dir string, args []string) ([]byte, error) {
		return []byte("ERROR: HTTP Error 429: Too Many Requests"), errors.New("exit status 1")
	}}
	b := &Baixador{Exec: fx, Bin: "yt-dlp", BaseDir: base}

	ped := pedidoTeste("antibot2")
	_, err := b.Baixar(context.Background(), ped)
	if !errors.Is(err, ErrAntiBot) {
		t.Fatalf("esperava ErrAntiBot, veio: %v", err)
	}
	// A mensagem tem que NOMEAR o problema (o operador precisa entender e saber o que fazer).
	if !strings.Contains(ped.Erro, "anti-robô") || !strings.Contains(ped.Erro, "aguarde") {
		t.Errorf("mensagem não nomeia o problema nem orienta: %q", ped.Erro)
	}
}

// Erro definitivo (vídeo indisponível) NÃO é refeito — insistir não ajudaria.
func TestBaixarErroDefinitivoNaoRefaz(t *testing.T) {
	_, esperas := capturaRetryDownload(t)
	base := t.TempDir()
	chamadas := 0
	fx := &fakeExec{handler: func(dir string, args []string) ([]byte, error) {
		chamadas++
		return []byte("ERROR: [youtube] xyz: Video unavailable"), errors.New("exit status 1")
	}}
	b := &Baixador{Exec: fx, Bin: "yt-dlp", BaseDir: base}
	if _, err := b.Baixar(context.Background(), pedidoTeste("indisp2")); !errors.Is(err, ErrVideoIndisponivel) {
		t.Fatalf("esperava ErrVideoIndisponivel, veio: %v", err)
	}
	if chamadas != 1 || len(*esperas) != 0 {
		t.Errorf("erro definitivo não deveria ser refeito: %d chamadas, esperas %v", chamadas, *esperas)
	}
}

func TestAntiBotDetecta(t *testing.T) {
	casos := map[string]bool{
		"ERROR: HTTP Error 429: Too Many Requests":                         true,
		"Sign in to confirm you’re not a bot":                              true, // apóstrofo tipográfico
		"Sign in to confirm you're not a bot":                              true, // apóstrofo simples
		"WARNING: [youtube] Unable to download webpage: Too Many Requests": true,
		"ERROR: [youtube] xyz: Video unavailable":                          false,
		"": false,
	}
	for entrada, quer := range casos {
		if got := antiBot([]byte(entrada)); got != quer {
			t.Errorf("antiBot(%q) = %v, quero %v", entrada, got, quer)
		}
	}
}

func TestBaixarVideoIndisponivel(t *testing.T) {
	base := t.TempDir()
	fx := &fakeExec{handler: func(dir string, args []string) ([]byte, error) {
		return []byte("ERROR: [youtube] xyz: Video unavailable"), errors.New("exit status 1")
	}}
	b := &Baixador{Exec: fx, Bin: "yt-dlp", BaseDir: base}

	ped := pedidoTeste("indisp")
	_, err := b.Baixar(context.Background(), ped)
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
	if !contem(leg, "--write-info-json") {
		t.Errorf("legenda deve pedir --write-info-json (para o título): %v", leg)
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
