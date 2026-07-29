package servidor

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"srtclean/internal/pipeline"
	"srtclean/internal/validacao"
)

// --- Mocks da fase pesada ---

type baixadorVideoFake struct {
	erro    error
	chamado bool
	mu      sync.Mutex
}

// O fake DEVOLVE a origem, como o baixador real: o arquivo que ele finge escrever é o vídeo
// inteiro, logo origem 0. Se um dia o fake e o real discordarem no valor, o teste do fluxo
// completo (que confere a origem em disco) acusa.
func (b *baixadorVideoFake) BaixarVideoCompleto(ctx context.Context, ped *pipeline.Pedido) (int, error) {
	b.mu.Lock()
	b.chamado = true
	b.mu.Unlock()
	if b.erro != nil {
		ped.Status = pipeline.EstadoErro
		ped.Erro = b.erro.Error()
		return 0, b.erro
	}
	return 0, nil
}

type renderFake struct {
	erro      error
	outDir    string
	origemMs  int
	nCands    int
	recebidos []validacao.Candidato // o que chegou ao render (spec-05 v2: tempos ajustados)
	mu        sync.Mutex
}

func (r *renderFake) RenderizarComOrigem(ctx context.Context, ped *pipeline.Pedido, cands []validacao.Candidato, origemMs int) ([]string, error) {
	r.mu.Lock()
	r.origemMs, r.nCands = origemMs, len(cands)
	r.recebidos = append([]validacao.Candidato(nil), cands...)
	r.mu.Unlock()
	if r.erro != nil {
		return nil, r.erro
	}
	dir := filepath.Join(r.outDir, ped.ID)
	os.MkdirAll(dir, 0755)
	var paths []string
	for i := range cands {
		p := filepath.Join(dir, fmt.Sprintf("short_%02d.mp4", i+1))
		os.WriteFile(p, []byte("mp4 fake"), 0644)
		paths = append(paths, p)
	}
	return paths, nil
}

func servidorPesada(t *testing.T, sel *selecionadorFake, bv *baixadorVideoFake, rf *renderFake) *Servidor {
	t.Helper()
	base := t.TempDir()
	out := t.TempDir()
	rf.outDir = out
	return Novo(Opcoes{
		Baixador:       &baixadorFake{transc: "[00:00:00] a graça basta.", base: base},
		Selecionador:   sel,
		BaixadorVideo:  bv,
		Renderizador:   rf,
		BaseDir:        base,
		OutDir:         out,
		LogRodadasPath: filepath.Join(base, "rodadas.md"), // isola: não escrever em resultados/
		TemposPath:     filepath.Join(base, "tempos.csv"),
		CortesPath:     filepath.Join(base, "cortes.csv"),
		Agora:          func() time.Time { return time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC) },
		GerarID:        func() string { return "teste-1" },
	})
}

// esperarArquivo aguarda um arquivo APARECER (escrito por uma goroutine). O status
// terminal é setado ANTES de persistir o CSV/limpar — de propósito: o operador vê o
// desfecho na hora e a faxina acontece atrás. Os testes, então, esperam o EFEITO.
func esperarArquivo(t *testing.T, path string) {
	t.Helper()
	prazo := time.Now().Add(2 * time.Second)
	for time.Now().Before(prazo) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("o arquivo %s não foi criado", path)
}

// esperarSumir aguarda um arquivo ser removido por uma goroutine (a limpeza roda depois
// de o status mudar, então checar na hora deixaria o teste flaky).
func esperarSumir(t *testing.T, path string) {
	t.Helper()
	prazo := time.Now().Add(2 * time.Second)
	for time.Now().Before(prazo) {
		if _, err := os.Stat(path); err != nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("o arquivo %s deveria ter sido limpo", filepath.Base(path))
}

func candsJanela() *selecionadorFake {
	return &selecionadorFake{cands: []validacao.Candidato{
		{Hook: "trecho 0", Start: "01:45:25.800", End: "01:46:10.200", DurationSeconds: 44, Score: 88},
		{Hook: "trecho 1", Start: "00:50:30.000", End: "00:51:00.000", DurationSeconds: 30, Score: 92},
		{Hook: "trecho 2", Start: "02:04:03.000", End: "02:04:37.500", DurationSeconds: 34, Score: 77},
	}}
}

// Fluxo completo: aprovar dispara a fase pesada; baixa o vídeo INTEIRO, o render recebe
// origem ZERO (tempo absoluto) e os Shorts ficam disponíveis para download.
func TestFaseHeavyFluxoCompleto(t *testing.T) {
	bv := &baixadorVideoFake{}
	rf := &renderFake{}
	s := servidorPesada(t, candsJanela(), bv, rf)
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)

	// Aprova os trechos 0 (01:45) e 2 (02:04).
	aprovarJSON(t, s, "teste-1", []int{0, 2})
	esperarStatus(t, s, "teste-1", pipeline.EstadoConcluido)

	// Baixou o vídeo inteiro (sem janela a calcular).
	bv.mu.Lock()
	baixou := bv.chamado
	bv.mu.Unlock()
	if !baixou {
		t.Error("a fase pesada deveria ter baixado o vídeo")
	}
	// CONTRATO DE TEMPO: com o vídeo inteiro, a origem é ZERO — o render corta em tempo
	// absoluto. Origem != 0 aqui significaria corte no lugar errado.
	rf.mu.Lock()
	origem, n := rf.origemMs, rf.nCands
	rf.mu.Unlock()
	if origem != 0 {
		t.Errorf("origem do render = %d, quero 0 (vídeo inteiro = tempo absoluto)", origem)
	}
	if n != 2 {
		t.Errorf("render recebeu %d candidatos, quero 2 (os aprovados)", n)
	}

	// E a origem fica GRAVADA no pedido.json, não só passada em memória: é o que permite ao
	// cmd/render (e ao cmd/auditar, e à retomada) saber depois que este video.mp4 é o vídeo
	// inteiro. Sem persistir, `cmd/render -id teste-1` voltaria a não ter o fato — que é
	// exatamente o bug relatado: Shorts da cena errada, deslocados por ped.Inicio, com a
	// duração correta. Ver spec-09.
	emDisco, err := pipeline.Carregar(s.baseDir, "teste-1")
	if err != nil {
		t.Fatalf("recarregando o pedido do disco: %v", err)
	}
	origemDisco, err := emDisco.Origem()
	if err != nil {
		t.Fatalf("o pedido em disco não declara a origem: %v", err)
	}
	if origemDisco != 0 {
		t.Errorf("origem_ms em disco = %d, quero 0 (vídeo inteiro)", origemDisco)
	}
	// E o Inicio continua o que o OPERADOR informou — a origem é um fato à parte, não um
	// apelido dele. (Este fixture manda 00:00:00 no formulário; o que importa é que o
	// servidor não reescreveu o campo para carregar a origem. O caso com Inicio != 0 está em
	// internal/download e em internal/video/origem_do_video_test.go.)
	if emDisco.Inicio != "00:00:00" || emDisco.Fim != "00:10:00" {
		t.Errorf("a janela informada pelo operador foi alterada: [%q–%q], esperado [00:00:00–00:10:00]",
			emDisco.Inicio, emDisco.Fim)
	}

	// Os Shorts aparecem na visão e são baixáveis.
	vis := statusJSONDoPedido(t, s, "teste-1")
	if vis.Status != string(pipeline.EstadoConcluido) {
		t.Errorf("status final = %q, quero concluido", vis.Status)
	}
	req := httptest.NewRequest("GET", "/finalizados/teste-1/short_01.mp4", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
		t.Errorf("download do short_01 falhou: código %d, %d bytes", rec.Code, rec.Body.Len())
	}
}

func TestFaseHeavyErroDownloadNaoTrava(t *testing.T) {
	bv := &baixadorVideoFake{erro: fmt.Errorf("googlevideo timeout")}
	rf := &renderFake{}
	s := servidorPesada(t, candsJanela(), bv, rf)
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, "teste-1", []int{0})
	esperarStatus(t, s, "teste-1", pipeline.EstadoErro) // erro visível, não spinner infinito

	vis := statusJSONDoPedido(t, s, "teste-1")
	if vis.Erro == "" {
		t.Error("falha no download deveria dar erro com mensagem, não travar")
	}
	rf.mu.Lock()
	renderChamado := rf.nCands
	rf.mu.Unlock()
	if renderChamado != 0 {
		t.Error("não deveria renderizar se o download falhou")
	}
}

func TestFaseHeavyErroRenderNaoTrava(t *testing.T) {
	bv := &baixadorVideoFake{}
	rf := &renderFake{erro: fmt.Errorf("ffmpeg: Invalid data")}
	s := servidorPesada(t, candsJanela(), bv, rf)
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, "teste-1", []int{0})
	esperarStatus(t, s, "teste-1", pipeline.EstadoErro)

	if vis := statusJSONDoPedido(t, s, "teste-1"); vis.Erro == "" {
		t.Error("falha no render deveria dar erro com mensagem")
	}
}

// Ao concluir, o servidor limpa o bruto dos pedidos ANTERIORES (spec-06) — sem isso,
// ~571 MB/pedido se acumulam até o disco encher. O pedido recém-concluído fica intacto.
func TestFaseHeavyLimpaPedidosAntigos(t *testing.T) {
	bv := &baixadorVideoFake{}
	rf := &renderFake{}
	s := servidorPesada(t, candsJanela(), bv, rf)

	// Um pedido ANTIGO com material bruto, já em disco.
	antigo := filepath.Join(s.baseDir, "pedido-antigo")
	os.MkdirAll(antigo, 0755)
	os.WriteFile(filepath.Join(antigo, "video.mp4"), make([]byte, 5000), 0644)
	os.WriteFile(filepath.Join(antigo, "candidatos.corrigido.json"), []byte("{}"), 0644)
	velho := time.Now().Add(-48 * time.Hour)
	os.Chtimes(filepath.Join(antigo, "candidatos.corrigido.json"), velho, velho)

	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, "teste-1", []int{0})
	esperarStatus(t, s, "teste-1", pipeline.EstadoConcluido)

	// A limpeza roda depois do status (assíncrona): esperar o efeito, não o estado.
	esperarSumir(t, filepath.Join(antigo, "video.mp4"))
	// ...mas o histórico auditável dele continua.
	if _, err := os.Stat(filepath.Join(antigo, "candidatos.corrigido.json")); err != nil {
		t.Error("candidatos.corrigido.json (fonte de verdade) foi apagado")
	}
}

func TestLimpezaDesligadaNaoApaga(t *testing.T) {
	bv := &baixadorVideoFake{}
	rf := &renderFake{}
	base := t.TempDir()
	out := t.TempDir()
	rf.outDir = out
	s := Novo(Opcoes{
		Baixador: &baixadorFake{transc: "x", base: base}, Selecionador: candsJanela(),
		BaixadorVideo: bv, Renderizador: rf, BaseDir: base, OutDir: out,
		LogRodadasPath: filepath.Join(base, "r.md"), TemposPath: filepath.Join(base, "t.csv"),
		CortesPath:       filepath.Join(base, "cortes.csv"),
		LimpezaDesligada: true,
		Agora:            func() time.Time { return time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC) },
		GerarID:          func() string { return "teste-1" },
	})
	antigo := filepath.Join(base, "pedido-antigo")
	os.MkdirAll(antigo, 0755)
	os.WriteFile(filepath.Join(antigo, "video.mp4"), make([]byte, 5000), 0644)
	velho := time.Now().Add(-48 * time.Hour)
	os.Chtimes(antigo, velho, velho)

	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, "teste-1", []int{0})
	esperarStatus(t, s, "teste-1", pipeline.EstadoConcluido)

	if _, err := os.Stat(filepath.Join(antigo, "video.mp4")); err != nil {
		t.Error("com a limpeza desligada, nada deveria ser apagado")
	}
}

// Um pedido que FALHA deixa lixo (mp4 parcial, .part do yt-dlp). Como falha costuma
// acontecer com o disco apertado, o resíduo é limpo na hora — senão o problema se
// realimenta: falhas acumulam e nunca são limpas.
func TestFaseHeavyLimpaResiduoDoPedidoQueFalhou(t *testing.T) {
	bv := &baixadorVideoFake{erro: fmt.Errorf("conexão caiu no meio")}
	rf := &renderFake{}
	s := servidorPesada(t, candsJanela(), bv, rf)

	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)

	// Simula o lixo que um download interrompido deixa na pasta do pedido.
	dir := filepath.Join(s.baseDir, "teste-1")
	os.WriteFile(filepath.Join(dir, "video.mp4.part"), make([]byte, 4000), 0644)
	os.WriteFile(filepath.Join(dir, "video.mp4.ytdl"), make([]byte, 100), 0644)

	aprovarJSON(t, s, "teste-1", []int{0})
	esperarStatus(t, s, "teste-1", pipeline.EstadoErro)

	// A limpeza do resíduo roda DEPOIS de o status virar erro (é assíncrona ao estado),
	// então esperar só o status deixaria o teste flaky. Aguarda o efeito observável.
	for _, lixo := range []string{"video.mp4.part", "video.mp4.ytdl"} {
		esperarSumir(t, filepath.Join(dir, lixo))
	}
	// A transcrição (histórico) sobrevive mesmo num pedido que falhou.
	if _, err := os.Stat(filepath.Join(dir, "transcricao.txt")); err != nil {
		t.Error("transcricao.txt não deveria ser apagada")
	}
}

// O download só serve arquivos que o pedido gerou (whitelist), nunca um caminho arbitrário.
func TestBaixarFinalRecusaArquivoForaDaWhitelist(t *testing.T) {
	bv := &baixadorVideoFake{}
	rf := &renderFake{}
	s := servidorPesada(t, candsJanela(), bv, rf)
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, "teste-1", []int{0})
	esperarStatus(t, s, "teste-1", pipeline.EstadoConcluido)

	// Um arquivo que o pedido não gerou → 404 (não vaza nem permite travessia).
	req := httptest.NewRequest("GET", "/finalizados/teste-1/segredo.txt", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("arquivo fora da whitelist deveria dar 404, veio %d", rec.Code)
	}
}
