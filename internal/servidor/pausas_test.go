package servidor

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"srtclean/internal/harness"
	"srtclean/internal/video"
	"srtclean/internal/videocache"
)

// O CASO REAL, virado teste (culto fZGyLBofmmo, 2026-07-30).
//
// A transcrição diz que o bloco seguinte começa em 01:30:05; o ÁUDIO diz que a fala corre sem
// parar de 01:30:04.126 até 01:30:09.486. O operador clicou na frase, o corte terminou em
// 01:30:06 — no meio da fala — e todos os deltas do histórico deram zero: o sistema aplicou
// fielmente um ponto que a LEGENDA inventou.
//
// Os números abaixo são os medidos, reduzidos para a escala da transcrição sintética dos testes.
const (
	fimDoBlocoDeLegenda = 44000 // o que o clique em frase pede (fronteira de bloco)
	pausaRealDoAudio    = 47500 // onde a fala de fato para, medida no áudio
)

func pausasDeTeste() []videocache.Pausa {
	return []videocache.Pausa{
		{InicioMs: 30000, FimMs: 30600},
		{InicioMs: 38000, FimMs: 38470},            // pausa curta ANTES do fim pedido
		{InicioMs: pausaRealDoAudio, FimMs: 49000}, // a fronteira certa
		{InicioMs: 60000, FimMs: 61500},
	}
}

// TestEncaixeDoCliqueVaiParaAPausaDoAudio é o teste do conserto, e é o que tem de falhar se
// alguém devolver o encaixe à fronteira de legenda.
//
// Verificação por mutação (feita): trocando encaixarFimComPausas por encaixarFim, este teste
// falha dizendo "o fim ficou em 44000 (fronteira de legenda), não na pausa do áudio (47500)".
func TestEncaixeDoCliqueVaiParaAPausaDoAudio(t *testing.T) {
	frases := frasesAjuste(t)
	ctx := ContextoAjuste{Pausas: pausasDeTeste(), Gesto: "frase-fim"}

	got := recalcularTrecho(frases, 0, 20000, fimDoBlocoDeLegenda, ctx)

	if got.EndMs != pausaRealDoAudio {
		t.Errorf("o fim ficou em %d (fronteira de legenda), não na pausa do áudio (%d): "+
			"é o bug do corte terminar no meio da fala", got.EndMs, pausaRealDoAudio)
	}
	if got.RegraFim != "pausa" {
		t.Errorf("regra_fim = %q, quero \"pausa\" — a tela e o histórico precisam saber QUEM "+
			"decidiu o fim", got.RegraFim)
	}
	if got.DeslocamentoFimMs != pausaRealDoAudio-fimDoBlocoDeLegenda {
		t.Errorf("deslocamento = %d ms, quero %d: é o número que a tela mostra para o operador "+
			"não descobrir o salto pelo ouvido", got.DeslocamentoFimMs,
			pausaRealDoAudio-fimDoBlocoDeLegenda)
	}
}

// TestEmpurraoFinoNaoEncaixaEmPausa é a outra metade da regra, e ela é igualmente essencial: se o
// empurrão encaixasse, cada clique de 0,25 s voltaria para a MESMA pausa e o ajuste fino deixaria
// de existir. As duas regras são diferentes de propósito — ver ContextoAjuste.
func TestEmpurraoFinoNaoEncaixaEmPausa(t *testing.T) {
	frases := frasesAjuste(t)
	ctx := ContextoAjuste{Pausas: pausasDeTeste(), Gesto: "fino-fim"}

	// Um empurrão que cai 250 ms depois do fim do bloco: perto de uma pausa, e ainda assim
	// tem de valer exatamente o que o operador pediu.
	const pedido = fimDoBlocoDeLegenda + 250
	got := recalcularTrecho(frases, 0, 20000, pedido, ctx)

	if got.EndMs != pedido {
		t.Errorf("o empurrão fino foi encaixado: pediu %d, ficou %d. Com isso o operador clica "+
			"0,25 s e nada muda", pedido, got.EndMs)
	}
	if got.RegraFim == "pausa" {
		t.Error("regra_fim = pausa num empurrão fino: as duas regras foram unificadas")
	}
}

// TestSemPausasCaiNaLegendaEDizIsso: culto ainda não analisado (não há vídeo em cache, logo não há
// áudio). O encaixe volta ao comportamento antigo — mas DECLARA a regra, em vez de o operador
// achar que está vendo fronteira de áudio.
func TestSemPausasCaiNaLegendaEDizIsso(t *testing.T) {
	frases := frasesAjuste(t)
	got := recalcularTrecho(frases, 0, 20000, fimDoBlocoDeLegenda, ContextoAjuste{Gesto: "frase-fim"})
	if got.RegraFim != "legenda" {
		t.Errorf("regra_fim = %q, quero \"legenda\": sem análise de pausas o operador tem de "+
			"saber que a fronteira veio do carimbo", got.RegraFim)
	}
}

// TestEncaixeSemLimiteDeDistancia: limitar a distância recriaria o bug. A pausa certa estava
// 3,5 s adiante no caso real; um teto de 1 s ou 2 s pararia o corte no meio da fala "porque
// estava longe".
func TestEncaixeSemLimiteDeDistancia(t *testing.T) {
	frases := frasesAjuste(t)
	// Pausa a 8 s do ponto pedido: muito mais que qualquer folga razoável.
	pausas := []videocache.Pausa{{InicioMs: 52000, FimMs: 53000}}
	got := recalcularTrecho(frases, 0, 20000, fimDoBlocoDeLegenda,
		ContextoAjuste{Pausas: pausas, Gesto: "frase-fim"})
	if got.EndMs != 52000 {
		t.Errorf("o fim ficou em %d: o encaixe desistiu por distância, que é exatamente o bug",
			got.EndMs)
	}
	// E se isso jogar a duração fora da faixa, o trecho fica NÃO aprovável com o motivo —
	// visível, nunca em silêncio.
	if got.DuracaoMs > 58000 && got.Aprovavel {
		t.Error("duração acima do máximo e ainda aprovável: a guarda desapareceu")
	}
}

// TestDeslocamentoGrandeApareceNaTela liga o número à interface: o campo tem de chegar ao
// cliente, senão o operador descobre o salto de 3,5 s pelo ouvido.
func TestDeslocamentoGrandeApareceNaTela(t *testing.T) {
	if !strings.Contains(htmlDaRevisao(t), `id="aj-encaixe"`) {
		t.Fatal("a tela não tem onde mostrar a mensagem do encaixe")
	}
	js := jsDaPagina(t)
	for _, quer := range []string{"deslocamento_fim_ms", "regra_fim", "pausa da fala"} {
		if !strings.Contains(js, quer) {
			t.Errorf("o cliente não usa %q — o deslocamento do encaixe não chega ao operador", quer)
		}
	}
}

// TestGestoViajaNoAjustar prova que o cliente MANDA o gesto: sem ele o servidor não distingue
// clique de empurrão, e a única saída seria encaixar tudo (matando o ajuste fino) ou nada
// (mantendo o bug).
func TestGestoViajaNoAjustar(t *testing.T) {
	if !strings.Contains(jsDaPagina(t), "gesto:") {
		t.Error("o /ajustar é chamado sem o campo gesto")
	}
	s := servidorAjuste(t)
	corpo := `{"indice":0,"start_ms":20000,"end_ms":44000,"gesto":"frase-fim"}`
	req := httptest.NewRequest("POST", "/pedidos/teste-1/ajustar", strings.NewReader(corpo))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("ajustar devolveu %d: %s", rec.Code, rec.Body.String())
	}
	var t2 TrechoAjustado
	if err := json.Unmarshal(rec.Body.Bytes(), &t2); err != nil {
		t.Fatal(err)
	}
	// Sem pausas em disco neste servidor de teste, a regra é "legenda" — o que importa aqui é
	// que o campo VEIO, e que o gesto foi aceito sem erro.
	if t2.RegraFim == "" {
		t.Error("a resposta não diz qual regra decidiu o fim")
	}
}

// --- geração e persistência das pausas ---

// analisadorFake devolve pausas fixas sem tocar em ffmpeg.
type analisadorFake struct {
	pausas  []video.Pausa
	erro    error
	chamado int
}

func (a *analisadorFake) Pausas(ctx context.Context, videoPath string) ([]video.Pausa, error) {
	a.chamado++
	return a.pausas, a.erro
}

// TestPausasSaoGeradasEGuardadasComAReceita: o arquivo tem de guardar OS PARÂMETROS, senão uma
// régua desenhada com um limiar e um encaixe calculado com outro discordariam na tela.
func TestPausasSaoGeradasEGuardadasComAReceita(t *testing.T) {
	s := servidorPesada(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})
	an := &analisadorFake{pausas: []video.Pausa{{InicioMs: 1000, FimMs: 1500}}}
	s.analisadorPausas = an
	s.pausasDB, s.pausasMinMs = -32, 300

	dir, err := s.cache.DirVideo("cultoTeste1")
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(dir, 0755)
	alvo := filepath.Join(dir, videocache.NomeVideo)
	if err := escreverVideoFalso(alvo); err != nil {
		t.Fatal(err)
	}

	s.garantirPausas(context.Background(), "cultoTeste1", alvo)
	a, err := s.cache.LerPausas("cultoTeste1")
	if err != nil {
		t.Fatalf("pausas.json não foi gravado: %v", err)
	}
	if len(a.Pausas) != 1 || a.Pausas[0].InicioMs != 1000 {
		t.Errorf("pausas gravadas erradas: %+v", a.Pausas)
	}
	if a.NoiseDB != -32 || a.MinMs != 300 {
		t.Errorf("a receita não foi gravada: db=%d min=%d", a.NoiseDB, a.MinMs)
	}

	// Segunda chamada não reanalisa: 6,5 s de ffmpeg por culto, não por pedido.
	s.garantirPausas(context.Background(), "cultoTeste1", alvo)
	if an.chamado != 1 {
		t.Errorf("analisou %d vezes; a análise é uma vez por culto", an.chamado)
	}
}

// TestReceitaDiferenteInvalidaAAnalise: mudar o limiar tem de refazer, não reaproveitar dado de
// origem desconhecida.
func TestReceitaDiferenteInvalidaAAnalise(t *testing.T) {
	s := servidorPesada(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})
	if err := s.cache.GravarPausas("cultoTeste1", videocache.AnalisePausas{
		NoiseDB: -40, MinMs: 800, Pausas: []videocache.Pausa{{InicioMs: 9, FimMs: 99}},
	}); err != nil {
		t.Fatal(err)
	}
	s.pausasDB, s.pausasMinMs = -32, 300

	s.mu.Lock()
	reg := s.pedidos["teste-1"]
	s.mu.Unlock()
	if reg == nil {
		criarPedidoOK(t, s)
		esperarStatus(t, s, "teste-1", "aguardando-aprovacao")
		s.mu.Lock()
		reg = s.pedidos["teste-1"]
		s.mu.Unlock()
	}
	if p := s.pausasDoPedido(reg); len(p) != 0 {
		t.Errorf("usou análise de receita diferente: %+v", p)
	}
}

// TestFalhaDaAnaliseNaoQuebraOPedido: sem pausas o encaixe cai na legenda, que é o que o sistema
// fazia até hoje — pior, mas não parado.
func TestFalhaDaAnaliseNaoQuebraOPedido(t *testing.T) {
	s := servidorPesada(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})
	s.analisadorPausas = &analisadorFake{erro: context.DeadlineExceeded}
	dir, _ := s.cache.DirVideo("cultoTeste1")
	os.MkdirAll(dir, 0755)
	alvo := filepath.Join(dir, videocache.NomeVideo)
	escreverVideoFalso(alvo)

	s.garantirPausas(context.Background(), "cultoTeste1", alvo) // não pode entrar em pânico
	if _, err := s.cache.LerPausas("cultoTeste1"); err == nil {
		t.Error("gravou pausas a partir de uma análise que falhou")
	}
}

// TestParsearSaidaDoSilencedetect protege o parse contra formato inesperado — é a fronteira com
// uma ferramenta externa, e o número que sai dela vira corte.
func TestParsearSaidaDoSilencedetect(t *testing.T) {
	frases := []harness.Frase{} // não usado; só para deixar claro que isto é parse puro
	_ = frases
	pausas := video.ParsearPausasParaTeste(`
[silencedetect @ 0x1] silence_start: 5403.659
[silencedetect @ 0x1] silence_end: 5404.126 | silence_duration: 0.467
[silencedetect @ 0x1] silence_start: 5409.486
[silencedetect @ 0x1] silence_end: 5411.0 | silence_duration: 1.514
[silencedetect @ 0x1] silence_start: 5999.9
`)
	if len(pausas) != 2 {
		t.Fatalf("esperava 2 pausas (a terceira não fechou), veio %d: %+v", len(pausas), pausas)
	}
	if pausas[0].InicioMs != 5403659 || pausas[0].FimMs != 5404126 {
		t.Errorf("pausa 1 = %+v, quero 5403659→5404126", pausas[0])
	}
	if pausas[1].DuracaoMs() != 1514 {
		t.Errorf("duração da pausa 2 = %d ms, quero 1514", pausas[1].DuracaoMs())
	}
}

// TestEncaixeQueEstouraAFaixaExplicaDeOndeVeio: o encaixe pode levar a duração além dos 58 s
// (a fala continua). O comportamento é encaixar, NÃO aprovar e DIZER que foi o encaixe — recuar
// para a pausa anterior em silêncio esconderia informação sobre o material.
func TestEncaixeQueEstouraAFaixaExplicaDeOndeVeio(t *testing.T) {
	frases := frasesAjuste(t)
	// Pausa longe o bastante para a duração passar do máximo (58 s).
	pausas := []videocache.Pausa{{InicioMs: 95000, FimMs: 96000}}
	got := recalcularTrecho(frases, 0, 20000, fimDoBlocoDeLegenda,
		ContextoAjuste{Pausas: pausas, Gesto: "frase-fim"})

	if got.EndMs != 95000 {
		t.Fatalf("o encaixe recuou em vez de ir à pausa: fim = %d", got.EndMs)
	}
	if got.Aprovavel {
		t.Error("trecho de 75 s ficou aprovável: a guarda da faixa desapareceu")
	}
	for _, quer := range []string{"o máximo é", "encaixe na pausa", "+51.00s"} {
		if !strings.Contains(got.Motivo, quer) {
			t.Errorf("o motivo não diz %q — o operador não sabe de onde vieram os segundos: %q",
				quer, got.Motivo)
		}
	}
}
