package servidor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"srtclean/internal/harness"
	"srtclean/internal/validacao"
)

// transcricaoAjuste monta uma transcrição sintética com frases de 6 s começando em 0:00,
// todas terminando em ponto (frases completas), para o encaixe ter onde cair.
func transcricaoAjuste() string {
	var b strings.Builder
	for i := 0; i < 30; i++ {
		seg := i * 6
		fmt.Fprintf(&b, "[%02d:%02d:%02d] frase numero %d termina aqui.\n", seg/3600, (seg%3600)/60, seg%60, i)
	}
	return b.String()
}

// transcricaoDeslocada começa em 00:00:30, então há uma faixa ANTES da primeira fronteira —
// a única região em que o encaixe para frente pode ser observado. Sem ela, a fixture normal
// (que começa em 0) torna o caso inalcançável, e um teste sobre ele passa sem provar nada.
func transcricaoDeslocada() string {
	var b strings.Builder
	for i := 0; i < 20; i++ {
		seg := 30 + i*6
		fmt.Fprintf(&b, "[%02d:%02d:%02d] deslocada numero %d termina aqui.\n", seg/3600, (seg%3600)/60, seg%60, i)
	}
	return b.String()
}

func frasesAjuste(t *testing.T) []harness.Frase {
	t.Helper()
	f := harness.Frasear(transcricaoAjuste())
	if len(f) < 20 {
		t.Fatalf("transcrição de teste rendeu só %d frases", len(f))
	}
	return f
}

// TestRecalculaHookTextoDuracao é o contrato central: dados tempos novos, o servidor devolve
// hook, duração e texto falado condizentes com a NOVA janela.
func TestRecalculaHookTextoDuracao(t *testing.T) {
	frases := frasesAjuste(t)

	// Janela de 0:36 a 1:18 = 42 s (dentro de 30–58).
	got := recalcularTrecho(frases, 0, 36000, 78000, ContextoAjuste{})
	if !got.Aprovavel {
		t.Fatalf("deveria ser aprovável: %s", got.Motivo)
	}
	if got.DuracaoMs != 42000 {
		t.Errorf("duração = %dms, queria 42000", got.DuracaoMs)
	}
	// Hook = a frase que começa em 0:36, ou seja, a de índice 6.
	if !strings.Contains(got.Hook, "frase numero 6") {
		t.Errorf("hook não é a frase de abertura da nova janela: %q", got.Hook)
	}
	// O texto tem de conter as frases da janela e NÃO as de fora.
	for _, quer := range []string{"frase numero 6", "frase numero 9", "frase numero 12"} {
		if !strings.Contains(got.TextoFalado, quer) {
			t.Errorf("texto falado não contém %q: %q", quer, got.TextoFalado)
		}
	}
	for _, naoQuer := range []string{"frase numero 5", "frase numero 13"} {
		if strings.Contains(got.TextoFalado, naoQuer) {
			t.Errorf("texto falado invadiu fora da janela (%q): %q", naoQuer, got.TextoFalado)
		}
	}
}

// TestHookRecalculadoAoEstenderParaTras é o caso que motivou a regra: estendendo o início, o
// hook DEIXA de ser a frase-âncora original e passa a ser a nova abertura. Se ficasse o
// antigo, o auditor acusaria "hook clipado" e a tela mentiria para o operador.
func TestHookRecalculadoAoEstenderParaTras(t *testing.T) {
	frases := frasesAjuste(t)

	original := recalcularTrecho(frases, 0, 60000, 102000, ContextoAjuste{}) // 1:00 → 1:42
	estendido := recalcularTrecho(frases, 0, 36000, 78000, ContextoAjuste{}) // recuou 24 s

	if original.Hook == estendido.Hook {
		t.Fatal("o hook não mudou ao estender para trás — a regra da Fase 3 não foi aplicada")
	}
	if !strings.Contains(estendido.Hook, "frase numero 6") {
		t.Errorf("hook novo não é a primeira frase da janela nova: %q", estendido.Hook)
	}
}

// TestInvarianteDoAuditorSeMantem: o start tem de cair DENTRO da frase do hook — entre o
// começo dela e o teto de folga, nunca antes. Substituiu a exigência de Δ=0, que era fiel à
// letra e infiel à intenção: com a legenda adiantada, começar um pouco depois do carimbo é
// frequentemente mais correto. Vale para tempos "tortos" marcados no player.
func TestInvarianteDoAuditorSeMantem(t *testing.T) {
	frases := frasesAjuste(t)

	// Tempos "tortos" em ms, como o player entregaria.
	for _, marcado := range []int{36000, 36400, 37900, 35200, 38700} {
		got := recalcularTrecho(frases, 0, marcado, marcado+42000, ContextoAjuste{})
		if got.Hook == "" {
			t.Fatalf("marcado %dms: sem hook", marcado)
		}
		idx, achou := harness.AcharAncora(frases, got.Hook)
		if !achou {
			t.Fatalf("marcado %dms: hook não encontrado na transcrição", marcado)
		}
		startMs, ok := validacao.HmsToMs(got.Start)
		if !ok {
			t.Fatalf("marcado %dms: start ilegível %q", marcado, got.Start)
		}
		folga := startMs - frases[idx].InicioMs
		if folga < 0 {
			t.Errorf("marcado %dms: start %dms ANTES da frase do hook — o auditor acusaria", marcado, -folga)
		}
		if folga > harness.FolgaInicioMaxMs {
			t.Errorf("marcado %dms: folga de %dms passa do teto — o auditor acusaria", marcado, folga)
		}
	}
}

// TestPontoMarcadoEhPreservadoNasDuasPontas: dentro da folga, o que o operador marcou é o que
// vale — nas duas pontas. Era o oposto no início (encaixava sempre), e esse encaixe anulava o
// principal caso de uso da ponta: ele clicava "mais tarde" e o sistema o devolvia.
func TestPontoMarcadoEhPreservadoNasDuasPontas(t *testing.T) {
	frases := frasesAjuste(t)

	got := recalcularTrecho(frases, 0, 37400, 79200, ContextoAjuste{})
	if got.StartMs != 37400 {
		t.Errorf("start movido para %dms: dentro da folga, o ponto marcado deveria valer", got.StartMs)
	}
	if got.EndMs != 79200 {
		t.Errorf("end movido para %dms: dentro da folga, o ponto marcado deveria valer", got.EndMs)
	}
	if got.AjustadoStart || got.AjustadoEnd {
		t.Error("nenhuma ponta foi movida; os avisos de encaixe não deveriam acender")
	}
}

// TestEncaixeSoAconteceParaFrente: o encaixe continua existindo, mas só na direção segura —
// quando o ponto marcado cai antes de qualquer fronteira. Usa a fixture deslocada, porque na
// normal (que começa em 0) não existe ponto antes da primeira fronteira.
func TestEncaixeSoAconteceParaFrente(t *testing.T) {
	frases := harness.Frasear(transcricaoDeslocada()) // fronteiras a partir de 30 s

	// Fim em 10 s: antes de qualquer fronteira. Tem de ir para FRENTE (30 s), nunca para trás.
	got := recalcularTrecho(frases, 0, 0, 10000, ContextoAjuste{})
	if got.EndMs < 10000 {
		t.Fatalf("o end foi para trás (%dms) — cortaria fala no meio", got.EndMs)
	}
	if got.EndMs != 30000 {
		t.Errorf("o end deveria encaixar na primeira fronteira (30000), foi para %dms", got.EndMs)
	}
	if !got.AjustadoEnd {
		t.Error("houve encaixe do fim; o aviso deveria acender para a tela poder explicar")
	}

	// O mesmo no início: ponto antes de qualquer frase encaixa para frente.
	got2 := recalcularTrecho(frases, 0, 5000, 70000, ContextoAjuste{})
	if got2.StartMs != 30000 {
		t.Errorf("o start deveria encaixar na primeira frase (30000), foi para %dms", got2.StartMs)
	}
	if !got2.AjustadoStart {
		t.Error("houve encaixe do início; o aviso deveria acender")
	}
	if !strings.Contains(got2.Hook, "deslocada numero 0") {
		t.Errorf("hook deveria ser a primeira frase: %q", got2.Hook)
	}
}

// TestFimLiberadoParaFrente é o caso que motivou o ajuste manual, e o que um encaixe
// simétrico anularia: o timestamp da legenda adianta o áudio, o operador ouve a palavra
// engolida e estende 2 s. Se o sistema o devolvesse à fronteira, o ajuste não serviria
// para nada — ele marca +2s e o sistema desfaz.
func TestFimLiberadoParaFrente(t *testing.T) {
	frases := frasesAjuste(t)

	// Fronteiras de 6 em 6 s. O operador marcou 2 s depois de uma delas.
	got := recalcularTrecho(frases, 0, 36000, 80000, ContextoAjuste{})
	if got.EndMs != 80000 {
		t.Errorf("o end foi movido para %dms — o operador marcou 80000 e a folga é segura", got.EndMs)
	}
	if got.AjustadoEnd {
		t.Error("AjustadoEnd marcado: o fim não deveria ter sido encaixado")
	}
	if !got.Aprovavel {
		t.Errorf("44 s deveria ser aprovável: %s", got.Motivo)
	}
}

// (O caso "fim antes de qualquer fronteira" está em TestEncaixeSoAconteceParaFrente, que usa
// a fixture deslocada. Na fixture normal a primeira fronteira é 0, então não existe ponto
// antes dela e a versão anterior deste teste passava sem exercitar nada.)

// TestFolgaDoFimTemTeto: folga é para sincronia de legenda, não licença para vazar. Além do
// teto, o sistema limita — senão o operador estica sem perceber e o auditor acusa depois.
func TestFolgaDoFimTemTeto(t *testing.T) {
	frases := frasesAjuste(t)

	// Última fronteira é 174 s (frase 29 começa em 174). Marcar 200 s pede 26 s de folga.
	got := recalcularTrecho(frases, 0, 150000, 200000, ContextoAjuste{})
	limite := 174000 + harness.FolgaFimMaxMs
	if got.EndMs > limite {
		t.Errorf("end = %dms passou do teto de folga (%dms)", got.EndMs, limite)
	}
}

// TestAjusteSobreviveAoAuditor fecha o ciclo com a spec-16: um trecho ajustado à mão, com
// folga de fim, NÃO pode ser acusado pelo auditor. Se este teste quebrar, o operador estaria
// gerando material que o próprio projeto marca como defeituoso.
func TestAjusteSobreviveAoAuditor(t *testing.T) {
	frases := frasesAjuste(t)
	got := recalcularTrecho(frases, 0, 36000, 80000, ContextoAjuste{}) // 2 s de folga no fim
	if !got.Aprovavel {
		t.Fatalf("pré-condição: %s", got.Motivo)
	}

	// Mesma verificação do cmd/auditar, item 2: existe fronteira completa em ou antes do
	// end, e a folga cabe no teto?
	endMs, _ := validacao.HmsToMs(got.End)
	fronteira := -1
	for _, f := range frases {
		if f.Completa && f.FimMs <= endMs && f.FimMs > fronteira {
			fronteira = f.FimMs
		}
	}
	if fronteira < 0 {
		t.Fatal("o auditor acusaria corte no meio da fala")
	}
	if folga := endMs - fronteira; folga > harness.FolgaFimMaxMs {
		t.Errorf("o auditor acusaria folga excessiva: %dms", folga)
	}
}

// TestGuardaDuracaoMinima e a máxima: a mensagem tem de trazer os números, não "fora da
// faixa". É o que permite ao operador consertar sem adivinhar.
func TestGuardaDuracaoForaDaFaixa(t *testing.T) {
	frases := frasesAjuste(t)

	curto := recalcularTrecho(frases, 0, 36000, 54000, ContextoAjuste{}) // 18 s
	if curto.Aprovavel {
		t.Error("18 s deveria ser recusado (mínimo 30 s)")
	}
	if !strings.Contains(curto.Motivo, "mínimo é 30") || !strings.Contains(curto.Motivo, "estenda") {
		t.Errorf("motivo não diz o que falta: %q", curto.Motivo)
	}

	longo := recalcularTrecho(frases, 0, 0, 66000, ContextoAjuste{}) // 66 s
	if longo.Aprovavel {
		t.Error("66 s deveria ser recusado (máximo 58 s)")
	}
	if !strings.Contains(longo.Motivo, "máximo é 58") || !strings.Contains(longo.Motivo, "encurte") {
		t.Errorf("motivo não diz o que sobra: %q", longo.Motivo)
	}
}

// TestFaixaVemDaHarness trava o alinhamento que o dono pediu: uma fonte só para a faixa de
// construção. Se alguém mudar a Fase 3, o ajuste manual acompanha.
func TestFaixaVemDaHarness(t *testing.T) {
	if _, motivo := duracaoAceitavel(harness.DuracaoMaxMs + 1); motivo == "" {
		t.Error("1 ms acima do máximo deveria ser recusado")
	}
	if ok, _ := duracaoAceitavel(harness.DuracaoMaxMs); !ok {
		t.Error("exatamente o máximo deveria passar")
	}
	if ok, _ := duracaoAceitavel(harness.DuracaoMinMs); !ok {
		t.Error("exatamente o mínimo deveria passar")
	}
	if _, motivo := duracaoAceitavel(harness.DuracaoMinMs - 1); motivo == "" {
		t.Error("1 ms abaixo do mínimo deveria ser recusado")
	}
}

// TestGuardaFimAntesDoInicio: end <= start é impedido, com mensagem em português.
func TestGuardaFimAntesDoInicio(t *testing.T) {
	frases := frasesAjuste(t)
	for _, c := range []struct{ ini, fim int }{{60000, 60000}, {60000, 30000}, {60000, 59900}} {
		got := recalcularTrecho(frases, 0, c.ini, c.fim, ContextoAjuste{})
		if got.Aprovavel {
			t.Errorf("start=%dms end=%dms deveria ser recusado", c.ini, c.fim)
		}
		if got.Motivo == "" {
			t.Errorf("start=%dms end=%dms: recusado sem explicar", c.ini, c.fim)
		}
	}
}

// TestClampNosLimitesDaPregacao: o ajuste não escapa da janela informada no pedido — fora
// dela está o louvor e os avisos, que o recorte existe para excluir.
func TestClampNosLimitesDaPregacao(t *testing.T) {
	frases := frasesAjuste(t)
	lim := LimitesPregacao{IniMs: 36000, FimMs: 120000} // 0:36 → 2:00

	got := recalcularTrecho(frases, 0, 0, 300000, ContextoAjuste{Lim: lim}) // tentou 0:00 → 5:00
	if got.StartMs < 36000 {
		t.Errorf("start escapou do limite inferior: %dms", got.StartMs)
	}
	if got.EndMs > 120000 {
		t.Errorf("end escapou do limite superior: %dms", got.EndMs)
	}
}

// --- Endpoint e /aprovar ---

// servidorAjuste prepara um servidor com a transcrição em disco, pronto para ajustar.
func servidorAjuste(t *testing.T) *Servidor {
	t.Helper()
	s := servidorPesada(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", "aguardando-aprovacao")
	// Sobrescreve a transcrição pela sintética, que tem frases suficientes para ajustar.
	p := filepath.Join(s.baseDir, "teste-1", "transcricao.txt")
	if err := os.WriteFile(p, []byte(transcricaoAjuste()), 0644); err != nil {
		t.Fatal(err)
	}
	return s
}

func postAjustar(t *testing.T, s *Servidor, indice, iniMs, fimMs int) (int, TrechoAjustado) {
	t.Helper()
	corpo, _ := json.Marshal(map[string]any{"indice": indice, "start_ms": iniMs, "end_ms": fimMs})
	req := httptest.NewRequest(http.MethodPost, "/pedidos/teste-1/ajustar", bytes.NewReader(corpo))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	var t2 TrechoAjustado
	json.Unmarshal(w.Body.Bytes(), &t2)
	return w.Code, t2
}

func TestEndpointAjustarDevolveTextoNovo(t *testing.T) {
	s := servidorAjuste(t)

	code, got := postAjustar(t, s, 0, 36000, 78000)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if got.DuracaoMs != 42000 {
		t.Errorf("duração = %dms, queria 42000", got.DuracaoMs)
	}
	if got.Hook == "" || got.TextoFalado == "" {
		t.Error("o operador precisa ver hook e texto novos antes de aprovar")
	}
	if !got.Aprovavel {
		t.Errorf("deveria ser aprovável: %s", got.Motivo)
	}
}

func TestEndpointAjustarRecusaTrechoInexistente(t *testing.T) {
	s := servidorAjuste(t)
	if code, _ := postAjustar(t, s, 99, 36000, 78000); code != http.StatusBadRequest {
		t.Errorf("status %d, queria 400", code)
	}
}

// TestAprovarUsaTemposAjustadosNoRender é o teste que fecha o ciclo: o render recebe o corte
// do OPERADOR, não o original. Sem isto, toda a funcionalidade é decorativa.
func TestAprovarUsaTemposAjustadosNoRender(t *testing.T) {
	bv := &baixadorVideoFake{}
	rf := &renderFake{}
	s := servidorPesada(t, candsJanela(), bv, rf)
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", "aguardando-aprovacao")
	os.WriteFile(filepath.Join(s.baseDir, "teste-1", "transcricao.txt"), []byte(transcricaoAjuste()), 0644)

	s.mu.Lock()
	origStart := s.pedidos["teste-1"].cands[0].Start
	s.mu.Unlock()

	corpo, _ := json.Marshal(map[string]any{
		"aprovados": []int{0},
		"ajustes":   []map[string]any{{"indice": 0, "start_ms": 36000, "end_ms": 78000}},
	})
	req := httptest.NewRequest(http.MethodPost, "/pedidos/teste-1/aprovar", bytes.NewReader(corpo))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("aprovar devolveu %d: %s", w.Code, w.Body)
	}
	esperarStatus(t, s, "teste-1", "concluido")

	rf.mu.Lock()
	recebidos := append([]validacao.Candidato(nil), rf.recebidos...)
	rf.mu.Unlock()
	if len(recebidos) != 1 {
		t.Fatalf("o render recebeu %d candidatos", len(recebidos))
	}
	c := recebidos[0]
	if c.Start == origStart {
		t.Errorf("o render recebeu o start ORIGINAL (%s) em vez do ajustado", origStart)
	}
	if c.Start != "00:00:36.000" {
		t.Errorf("start no render = %q, queria 00:00:36.000", c.Start)
	}
	if c.DurationSeconds != 42 {
		t.Errorf("duração no render = %.1f, queria 42", c.DurationSeconds)
	}
	if !strings.Contains(c.Hook, "frase numero 6") {
		t.Errorf("o render recebeu o hook antigo: %q", c.Hook)
	}
}

// TestAprovarRecusaAjusteForaDaFaixa: a guarda vale no SERVIDOR. Um POST direto não pode
// enfiar um corte de 66 s no render só porque burlou o cliente.
func TestAprovarRecusaAjusteForaDaFaixa(t *testing.T) {
	s := servidorAjuste(t)

	corpo, _ := json.Marshal(map[string]any{
		"aprovados": []int{0},
		"ajustes":   []map[string]any{{"indice": 0, "start_ms": 0, "end_ms": 66000}},
	})
	req := httptest.NewRequest(http.MethodPost, "/pedidos/teste-1/aprovar", bytes.NewReader(corpo))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, queria 400 — a guarda do servidor não pegou", w.Code)
	}
	if !strings.Contains(w.Body.String(), "58") {
		t.Errorf("a recusa não diz o limite: %s", w.Body)
	}
}

// TestAprovarIgnoraAjusteDeTrechoNaoAprovado: mexer e desistir não é erro.
func TestAprovarIgnoraAjusteDeTrechoNaoAprovado(t *testing.T) {
	s := servidorAjuste(t)

	corpo, _ := json.Marshal(map[string]any{
		"aprovados": []int{0},
		// Ajuste absurdo, mas do trecho 1, que NÃO está aprovado.
		"ajustes": []map[string]any{{"indice": 1, "start_ms": 0, "end_ms": 500000}},
	})
	req := httptest.NewRequest(http.MethodPost, "/pedidos/teste-1/aprovar", bytes.NewReader(corpo))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status %d: ajuste de trecho não aprovado não deveria bloquear", w.Code)
	}
	// Espera a fase pesada acabar: senão ela escreve na pasta depois do teste e o
	// t.TempDir falha ao limpar.
	esperarStatus(t, s, "teste-1", "concluido")
}

// --- Cliente (estrutura servida) ---
//
// Não há navegador nos testes, então o que se verifica aqui é o CONTRATO da tela: os
// controles existem, a granularidade é a combinada e o JS aponta para o endpoint certo. É o
// que pega uma regressão de renomeação ou de rota, que quebraria o ajuste em silêncio.

// htmlDaRevisao devolve a MARCAÇÃO da tela de revisão.
//
// Desde a spec-05 v3 ela vive na PÁGINA (GET /), não no fragmento: as quatro telas ficam no
// DOM desde o carregamento, e o fragmento do servidor passou a trazer só dado. Este helper
// mudou de rota junto — e a mudança é o próprio ponto da parte 1, então os testes de contrato
// da tela continuam válidos: eles verificam que os controles existem onde o JS vai buscá-los.
func htmlDaRevisao(t *testing.T) string {
	t.Helper()
	return htmlDaPagina(t)
}

// htmlDoEstado devolve o FRAGMENTO que o servidor troca (GET /pedidos/{id}): desde a
// spec-05 v3 ele traz dado (estado-json, dados-trechos), não marcação de tela.
func htmlDoEstado(t *testing.T) string {
	t.Helper()
	s := servidorPesada(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", "aguardando-aprovacao")
	req := httptest.NewRequest(http.MethodGet, "/pedidos/teste-1", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w.Body.String()
}

// htmlDaPagina devolve a página inteira (GET /), onde vive o JS.
func htmlDaPagina(t *testing.T) string {
	t.Helper()
	s := servidorPesada(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w.Body.String()
}

func TestTelaTrazControlesDeAjuste(t *testing.T) {
	corpo := htmlDaRevisao(t)
	for _, quer := range []string{
		`id="aj-frases"`,                 // a faixa de frases clicável
		`id="aj-instrucao"`,              // o passo do fluxo de dois cliques, ANTES do clique
		`id="aj-v-ini"`, `id="aj-v-fim"`, // o valor ENTRE as setas que o mudam
		`id="aj-ini-mm"`, `id="aj-ini-m"`, `id="aj-ini-p"`, `id="aj-ini-pp"`,
		`id="aj-fim-mm"`, `id="aj-fim-m"`, `id="aj-fim-p"`, `id="aj-fim-pp"`,
		`id="aj-restaurar"`, `id="aj-resumo"`, `id="aj-invalido"`, `id="btn-meia"`,
		`id="aj-tocando"`,      // selo: de onde veio o som
		`class="duas-colunas"`, // layout numa tela
	} {
		if !strings.Contains(corpo, quer) {
			t.Errorf("o fragmento de revisão não trouxe %q", quer)
		}
	}
}

// TestSemFalsaPrecisaoNaTela: "54.75s" e "00:40:12.000" anunciam precisão que o sistema não
// tem — a transcrição vem em segundos inteiros. Mesma falsa precisão já rejeitada na grade.
func TestSemFalsaPrecisaoNaTela(t *testing.T) {
	frases := frasesAjuste(t)
	got := recalcularTrecho(frases, 0, 36000, 80000, ContextoAjuste{})

	// Os rótulos da vizinhança são o que a tela mostra: sem milissegundos.
	if len(got.Vizinhanca) == 0 {
		t.Fatal("sem vizinhança não há faixa de frases")
	}
	for _, f := range got.Vizinhanca {
		if strings.Contains(f.Rotulo, ".") {
			t.Errorf("rótulo com fração de segundo: %q", f.Rotulo)
		}
	}
	// As mensagens de guarda também: segundos inteiros.
	curto := recalcularTrecho(frases, 0, 36000, 54000, ContextoAjuste{})
	if strings.Contains(curto.Motivo, ".") {
		t.Errorf("mensagem com falsa precisão: %q", curto.Motivo)
	}
}

// TestVizinhancaMarcaDentroEFora é o contrato da faixa clicável: as frases do corte vêm
// marcadas, e há contexto dos dois lados para o operador apontar onde a ideia começa/termina.
func TestVizinhancaMarcaDentroEFora(t *testing.T) {
	frases := frasesAjuste(t)
	got := recalcularTrecho(frases, 0, 36000, 78000, ContextoAjuste{})

	var dentro, antes, depois int
	for _, f := range got.Vizinhanca {
		switch {
		case f.Dentro:
			dentro++
		case f.InicioMs < 36000:
			antes++
		default:
			depois++
		}
	}
	if dentro == 0 {
		t.Error("nenhuma frase marcada como dentro do corte — nada ficaria destacado")
	}
	if antes == 0 || depois == 0 {
		t.Errorf("falta contexto: %d antes, %d depois — o operador precisa ver os dois lados", antes, depois)
	}
	// Ordem cronológica: a faixa é lida de cima para baixo.
	for i := 1; i < len(got.Vizinhanca); i++ {
		if got.Vizinhanca[i].InicioMs < got.Vizinhanca[i-1].InicioMs {
			t.Fatal("vizinhança fora de ordem cronológica")
		}
	}
}

// TestVizinhancaVemMesmoComAjusteInvalido: é justamente quando o operador precisa da faixa
// para se orientar. Um painel que esvazia no erro deixa ele sem saída.
func TestVizinhancaVemMesmoComAjusteInvalido(t *testing.T) {
	frases := frasesAjuste(t)
	for _, c := range []struct {
		nome     string
		ini, fim int
	}{
		{"duração curta", 36000, 54000},
		{"duração longa", 0, 66000},
		{"fim antes do início", 60000, 30000},
	} {
		got := recalcularTrecho(frases, 0, c.ini, c.fim, ContextoAjuste{})
		if got.Aprovavel {
			t.Fatalf("%s: deveria ser inválido", c.nome)
		}
		if len(got.Vizinhanca) == 0 {
			t.Errorf("%s: faixa de frases vazia — o operador fica sem referência para consertar", c.nome)
		}
	}
}

// TestEndpointAjustarAceitaFimComFolga é o caminho completo do caso de uso, pelo HTTP: o
// operador estende 2 s além da fronteira e o servidor aceita, devolvendo o texto novo.
func TestEndpointAjustarAceitaFimComFolga(t *testing.T) {
	s := servidorAjuste(t)

	code, got := postAjustar(t, s, 0, 36000, 80000) // fronteiras de 6 em 6: 78 + 2 s de folga
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if got.EndMs != 80000 {
		t.Errorf("o servidor moveu o fim para %dms — a folga de 2s deveria ser aceita", got.EndMs)
	}
	if !got.Aprovavel {
		t.Errorf("deveria ser aprovável: %s", got.Motivo)
	}
	if got.TextoFalado == "" {
		t.Error("sem texto falado o operador ajusta às cegas")
	}
}

// AQUI VIVIA o TestSeekToSegueARecomendacaoDoYouTube, que exigia allowSeekAhead=false nos
// empurrões e true quando o debounce assentava — recomendação da documentação da IFrame API para
// não disparar uma requisição por clique.
//
// Ele SAIU com a API (spec-05 v4, fatia 2), e a lição fica: aquela regra existia porque o seek era
// REMOTO e caro. Num <video> local o currentTime é atribuição direta e exata, então não há
// "barato" contra "de verdade" — a distinção deixou de existir junto com o não-determinismo que a
// motivava (o seekTo do YouTube pousa no keyframe mais próximo "a menos que a porção já esteja
// bufferizada"; medido: +89 ms de overshoot na parada).
//
// O que passou a proteger este trecho é o TestPlayerLocalSemAPIDoYouTube, abaixo: nenhuma chamada
// da API remota pode voltar à tela.

// TestPlayerLocalSemAPIDoYouTube trava a fatia 2: o preview usa o MESMO arquivo que o corte, e a
// IFrame API não volta por descuido. Duas fontes de tempo é a discrepância que esta fatia
// eliminou por construção — reintroduzir uma chamada da API a traria de volta em silêncio.
func TestPlayerLocalSemAPIDoYouTube(t *testing.T) {
	pagina := htmlDaPagina(t)
	for _, proibido := range []string{
		"iframe_api", "YT.Player", "YT.PlayerState", "getPlayerState()",
		".seekTo(", ".playVideo()", ".pauseVideo()", ".getCurrentTime()", ".setPlaybackRate(",
	} {
		if strings.Contains(pagina, proibido) {
			t.Errorf("a tela voltou a usar %q: o preview tem de vir do arquivo local, não do "+
				"player do YouTube", proibido)
		}
	}
	// E o player local tem de estar lá, apontando para a rota do nosso servidor.
	for _, quer := range []string{`<video id="palco"`, "'/video/' + encodeURIComponent"} {
		if !strings.Contains(pagina, quer) {
			t.Errorf("a tela não tem %q — sem isso não há escuta do arquivo que será cortado", quer)
		}
	}
}

// TestTemposInternosEmMilissegundos: guardar em float de segundos acumularia erro numa rajada
// de empurrões de 0,25s, e o tempo é a chave do corte.
func TestTemposInternosEmMilissegundos(t *testing.T) {
	js := htmlDaPagina(t)
	for _, quer := range []string{"iniMs", "fimMs", "start_ms: a.iniMs"} {
		if !strings.Contains(js, quer) {
			t.Errorf("o cliente não guarda os tempos em ms: falta %q", quer)
		}
	}
	if strings.Contains(js, "a.startSeg + ','") {
		t.Error("o envio voltou a usar segundos float")
	}

	// E o servidor: uma rajada de 0,25s não pode derivar. Simula 8 empurrões.
	frases := frasesAjuste(t)
	ini, fim := 36000, 78000
	for i := 0; i < 8; i++ {
		got := recalcularTrecho(frases, 0, ini, fim+250, ContextoAjuste{})
		ini, fim = got.StartMs, got.EndMs
	}
	if fim != 80000 {
		t.Errorf("após 8 empurrões de 250ms o fim é %dms, esperado exatamente 80000", fim)
	}
}

// --- Início liberado para frente (spec-05 v2, correção da ponta espelhada) ---

// TestInicioLiberadoParaFrente é o caso relatado pelo operador: o corte em 00:20:08 ainda
// deixava ouvir o rabo da fala anterior, ele clicava "mais tarde" e o encaixe o devolvia. A
// legenda adianta o áudio nas DUAS pontas.
func TestInicioLiberadoParaFrente(t *testing.T) {
	frases := frasesAjuste(t)

	// Fronteiras de 6 em 6 s. O operador empurrou o início 2 s adiante do carimbo.
	got := recalcularTrecho(frases, 0, 38000, 80000, ContextoAjuste{})
	if got.StartMs != 38000 {
		t.Errorf("o início foi devolvido para %dms — o operador marcou 38000 e a folga é segura", got.StartMs)
	}
	if got.AjustadoStart {
		t.Error("AjustadoStart marcado: o início não deveria ter sido movido")
	}
}

// TestHookEhAFraseQueContemOStart é a diferença crucial em relação ao fim: empurrar o início
// para frente NÃO pula para a próxima frase. Com o carimbo adiantado, a frase que contém o
// start pelo carimbo é a que se OUVE — e é ela que tem de ser o hook.
func TestHookEhAFraseQueContemOStart(t *testing.T) {
	frases := frasesAjuste(t)

	// A frase 6 está carimbada em 36 s; a 7 em 42 s. Start em 38 s: dentro da 6.
	got := recalcularTrecho(frases, 0, 38000, 80000, ContextoAjuste{})
	if !strings.Contains(got.Hook, "frase numero 6") {
		t.Errorf("o hook pulou para a frase seguinte: %q — deveria ser a que contém o start", got.Hook)
	}
	// E o texto falado precisa COMEÇAR nela: com o start adiante do carimbo, usar o start como
	// fronteira do texto deixaria a própria frase do hook de fora.
	if !strings.HasPrefix(got.TextoFalado, "frase numero 6") {
		t.Errorf("o texto não começa no hook: %q", got.TextoFalado)
	}
}

// TestFraseDoStartFicaDestacadaNaFaixa: se a frase do hook não vier marcada como dentro do
// corte, a faixa mostraria o hook em cinza — contradizendo o que o operador vai gerar.
func TestFraseDoStartFicaDestacadaNaFaixa(t *testing.T) {
	frases := frasesAjuste(t)
	got := recalcularTrecho(frases, 0, 38000, 80000, ContextoAjuste{})

	achou := false
	for _, f := range got.Vizinhanca {
		if strings.Contains(f.Texto, "frase numero 6") {
			achou = true
			if !f.Dentro {
				t.Error("a frase que contém o start não está destacada como dentro do corte")
			}
		}
	}
	if !achou {
		t.Fatal("a frase do hook não apareceu na faixa")
	}
}

// (O caso "início antes de qualquer fronteira" também está em TestEncaixeSoAconteceParaFrente,
// pelo mesmo motivo: na fixture normal a primeira frase começa em 0.)

// TestFolgaDoInicioTemTeto: folga é para sincronia da legenda, não licença para abrir no meio
// da frase.
func TestFolgaDoInicioTemTeto(t *testing.T) {
	frases := frasesAjuste(t)

	// Frase 6 carimbada em 36 s; pedir início em 41 s pede 5 s (no limite) e em 41,5 s passa.
	noLimite := recalcularTrecho(frases, 0, 36000+harness.FolgaInicioMaxMs, 90000, ContextoAjuste{})
	if noLimite.StartMs != 36000+harness.FolgaInicioMaxMs {
		t.Errorf("exatamente no teto deveria passar: %dms", noLimite.StartMs)
	}

	// Um caso onde a folga estoura precisa de um vão sem frases; a frase 29 (174 s) é a
	// última, então qualquer ponto muito depois dela cai nesse vão.
	longe := recalcularTrecho(frases, 0, 174000+20000, 174000+60000, ContextoAjuste{})
	if longe.StartMs > 174000+harness.FolgaInicioMaxMs {
		t.Errorf("start = %dms passou do teto de folga do início", longe.StartMs)
	}
}

// TestAjusteDeInicioSobreviveAoAuditor fecha o ciclo com a spec-16 na ponta do início: um
// trecho com início empurrado não pode ser acusado pelo próprio projeto.
func TestAjusteDeInicioSobreviveAoAuditor(t *testing.T) {
	frases := frasesAjuste(t)
	got := recalcularTrecho(frases, 0, 38000, 80000, ContextoAjuste{}) // 2 s de folga no início
	if !got.Aprovavel {
		t.Fatalf("pré-condição: %s", got.Motivo)
	}

	// Mesma verificação do cmd/auditar, item 1.
	idx, achou := harness.AcharAncora(frases, got.Hook)
	if !achou {
		t.Fatal("o auditor não acharia o hook")
	}
	folga := got.StartMs - frases[idx].InicioMs
	if folga < 0 {
		t.Errorf("o auditor acusaria start antes da frase do hook (%dms)", folga)
	}
	if folga > harness.FolgaInicioMaxMs {
		t.Errorf("o auditor acusaria folga excessiva no start (%dms)", folga)
	}
}

// --- CSV de ajustes: acumular o dado, sem agir sobre ele ---

// TestCSVRegistraAsQuatroPontas: o valor do arquivo é permitir olhar, depois de uns dez
// trechos, se o desvio da legenda é consistente. Para isso precisa das quatro pontas (start e
// end, original e final) e dos dois deltas.
func TestCSVRegistraAsQuatroPontas(t *testing.T) {
	s := servidorAjuste(t)

	s.mu.Lock()
	origStart, origEnd := s.pedidos["teste-1"].cands[0].Start, s.pedidos["teste-1"].cands[0].End
	s.mu.Unlock()
	iniOrig, _ := validacao.HmsToMs(origStart)
	fimOrig, _ := validacao.HmsToMs(origEnd)

	corpo, _ := json.Marshal(map[string]any{
		"aprovados": []int{0},
		"ajustes":   []map[string]any{{"indice": 0, "start_ms": 38000, "end_ms": 80000}},
	})
	req := httptest.NewRequest(http.MethodPost, "/pedidos/teste-1/aprovar", bytes.NewReader(corpo))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("aprovar devolveu %d: %s", w.Code, w.Body)
	}
	esperarStatus(t, s, "teste-1", "concluido")

	b, err := os.ReadFile(s.cortesPath)
	if err != nil {
		t.Fatalf("CSV de ajustes não foi criado: %v", err)
	}
	texto := string(b)
	linhas := strings.Split(strings.TrimSpace(texto), "\n")
	if len(linhas) != 2 {
		t.Fatalf("esperava cabeçalho + 1 linha, veio %d:\n%s", len(linhas), texto)
	}

	// Cabeçalho com as quatro pontas e os dois deltas.
	for _, col := range []string{"ajustado", "start_original", "start_final", "delta_start_ms",
		"end_original", "end_final", "delta_end_ms"} {
		if !strings.Contains(linhas[0], col) {
			t.Errorf("cabeçalho sem a coluna %q: %s", col, linhas[0])
		}
	}

	campos := strings.Split(linhas[1], ",")
	if len(campos) != strings.Count(cabecalhoCortes, ",")+1 {
		t.Fatalf("linha com %d campos, cabeçalho tem %d:\n%s",
			len(campos), strings.Count(cabecalhoCortes, ",")+1, linhas[1])
	}
	if campos[1] != "teste-1" || campos[2] != "0" {
		t.Errorf("pedido/índice errados: %q, %q", campos[1], campos[2])
	}
	if campos[3] != "sim" {
		t.Errorf("coluna ajustado = %q, esperado \"sim\"", campos[3])
	}
	// Os deltas são a medição que interessa: quanto o operador moveu cada ponta.
	if campos[6] != strconv.Itoa(38000-iniOrig) {
		t.Errorf("delta_start = %q, esperado %d", campos[6], 38000-iniOrig)
	}
	if campos[9] != strconv.Itoa(80000-fimOrig) {
		t.Errorf("delta_end = %q, esperado %d", campos[9], 80000-fimOrig)
	}
}

// TestCSVRegistraAprovadoSEMAjuste é a correção do viés de seleção, e o teste mais importante
// deste arquivo. Registrar só os ajustados montaria uma amostra apenas dos casos ruins: o
// trecho aprovado sem ajuste é a evidência de que o corte estava BOM.
func TestCSVRegistraAprovadoSEMAjuste(t *testing.T) {
	s := servidorAjuste(t)

	aprovarJSON(t, s, "teste-1", []int{0})
	esperarStatus(t, s, "teste-1", "concluido")

	b, err := os.ReadFile(s.cortesPath)
	if err != nil {
		t.Fatalf("aprovado sem ajuste não gerou linha — a amostra ficaria só com os casos ruins: %v", err)
	}
	linhas := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(linhas) != 2 {
		t.Fatalf("esperava cabeçalho + 1 linha, veio %d:\n%s", len(linhas), b)
	}
	campos := strings.Split(linhas[1], ",")
	if campos[3] != "nao" {
		t.Errorf("coluna ajustado = %q, esperado \"nao\"", campos[3])
	}
	// Deltas zero: é o que corrige a média.
	if campos[6] != "0" || campos[9] != "0" {
		t.Errorf("deltas deveriam ser 0 no trecho não ajustado: start=%q end=%q", campos[6], campos[9])
	}
	// E os tempos finais são os originais.
	if campos[4] != campos[5] || campos[7] != campos[8] {
		t.Errorf("sem ajuste, final deveria igualar original: %v", campos[4:9])
	}
}

// TestCSVCorrigeAMediaComOsNaoAjustados reproduz o exemplo numérico que motivou a correção:
// 10 aprovados, 3 com +2s no fim. Sobre os ajustados a média é 2s; a média real é 0,6s.
// Aplicar 2s na Fase 3 empurraria os 7 corretos para longe demais.
func TestCSVCorrigeAMediaComOsNaoAjustados(t *testing.T) {
	s := servidorAjuste(t)
	frases := frasesAjuste(t)
	reg := s.pedidos["teste-1"]

	// 10 candidatos com os mesmos tempos-base, dentro da faixa da transcrição sintética
	// (fronteiras de 6 em 6 s, até 174 s).
	const iniOrig, fimOrig = 36000, 78000
	s.mu.Lock()
	base := reg.cands[0]
	base.Start, base.End = hms(iniOrig), hms(fimOrig)
	reg.cands = reg.cands[:0]
	for len(reg.cands) < 10 {
		reg.cands = append(reg.cands, base)
	}
	s.mu.Unlock()

	// 3 ajustados em +2s no fim; os outros 7 aprovados como estão.
	ajustes := map[int]TrechoAjustado{}
	for i := 0; i < 3; i++ {
		t1 := recalcularTrecho(frases, i, iniOrig, fimOrig+2000, ContextoAjuste{})
		if !t1.Aprovavel {
			t.Fatalf("caso %d inválido: %s", i, t1.Motivo)
		}
		ajustes[i] = t1
	}
	todos := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	s.registrarCortes(reg, todos, ajustes)

	b, err := os.ReadFile(s.cortesPath)
	if err != nil {
		t.Fatal(err)
	}
	linhas := strings.Split(strings.TrimSpace(string(b)), "\n")[1:]
	if len(linhas) != 10 {
		t.Fatalf("esperava 10 linhas (todos os aprovados), veio %d", len(linhas))
	}

	var soma, n, ajustados int
	for _, l := range linhas {
		c := strings.Split(l, ",")
		d, err := strconv.Atoi(c[9])
		if err != nil {
			t.Fatalf("delta_end ilegível em %q", l)
		}
		soma += d
		n++
		if c[3] == "sim" {
			ajustados++
		}
	}
	if ajustados != 3 {
		t.Errorf("coluna ajustado marcou %d, esperado 3", ajustados)
	}
	// A média sobre TODOS é o número que se aplicaria na Fase 3.
	media := soma / n
	if media > 900 {
		t.Errorf("média = %dms: com os não ajustados de fora daria ~2000ms e empurraria os corretos", media)
	}
	// E a proporção sai de graça: o indicador de saúde do sistema.
	if prop := ajustados * 100 / n; prop != 30 {
		t.Errorf("proporção de ajustados = %d%%, esperado 30%%", prop)
	}
}

// TestCSVDeAjustesAnexaSemRepetirCabecalho: o arquivo acumula ao longo de vários pedidos, e é
// essa acumulação que permite avaliar consistência.
func TestCSVAnexaSemRepetirCabecalho(t *testing.T) {
	s := servidorAjuste(t)
	frases := frasesAjuste(t)

	reg := s.pedidos["teste-1"]
	for i, par := range [][2]int{{38000, 80000}, {36000, 78000}, {42000, 84000}} {
		t1 := recalcularTrecho(frases, 0, par[0], par[1], ContextoAjuste{})
		if !t1.Aprovavel {
			t.Fatalf("caso %d: %s", i, t1.Motivo)
		}
		s.registrarCortes(reg, []int{0}, map[int]TrechoAjustado{0: t1})
	}

	b, err := os.ReadFile(s.cortesPath)
	if err != nil {
		t.Fatal(err)
	}
	linhas := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(linhas) != 4 {
		t.Fatalf("esperava cabeçalho + 3 linhas, veio %d", len(linhas))
	}
	if strings.Count(string(b), "delta_start_ms") != 1 {
		t.Error("cabeçalho repetido: quebraria a leitura do CSV")
	}
}

// TestCSVNaoRegistraTrechoREPROVADO: reprovado não é medição de corte — o operador rejeitou o
// CONTEÚDO, não o recorte. Incluí-lo misturaria duas coisas diferentes na mesma coluna.
func TestCSVNaoRegistraTrechoREPROVADO(t *testing.T) {
	s := servidorAjuste(t)

	aprovarJSON(t, s, "teste-1", []int{1}) // aprova só o índice 1
	esperarStatus(t, s, "teste-1", "concluido")

	b, err := os.ReadFile(s.cortesPath)
	if err != nil {
		t.Fatal(err)
	}
	linhas := strings.Split(strings.TrimSpace(string(b)), "\n")[1:]
	if len(linhas) != 1 {
		t.Fatalf("esperava 1 linha (só o aprovado), veio %d:\n%s", len(linhas), b)
	}
	if c := strings.Split(linhas[0], ","); c[2] != "1" {
		t.Errorf("registrou o índice %q em vez do aprovado (1)", c[2])
	}
}

// TestFalhaAoRegistrarNaoQuebraOPedido: é dado de pesquisa. O Short do operador vale mais que
// a estatística.
func TestFalhaAoRegistrarNaoQuebraOPedido(t *testing.T) {
	s := servidorAjuste(t)
	// Caminho impossível de escrever (um arquivo comum no lugar do diretório-pai).
	bloqueio := filepath.Join(t.TempDir(), "arquivo")
	os.WriteFile(bloqueio, []byte("x"), 0644)
	s.cortesPath = filepath.Join(bloqueio, "cortes.csv")

	corpo, _ := json.Marshal(map[string]any{
		"aprovados": []int{0},
		"ajustes":   []map[string]any{{"indice": 0, "start_ms": 38000, "end_ms": 80000}},
	})
	req := httptest.NewRequest(http.MethodPost, "/pedidos/teste-1/aprovar", bytes.NewReader(corpo))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("falha ao registrar quebrou o pedido: %d", w.Code)
	}
	esperarStatus(t, s, "teste-1", "concluido")
}

// --- Correções da segunda rodada com o operador ---

// TestNenhumaEscutaPassaDoCorte é o item 1, e a raiz do "o ajuste não pegou": "ouvir o fim"
// tocava até (fim + 2s) e soava perfeito, enquanto "tocar do início" parava em `fim` — o que o
// Short de fato terá. As duas discordavam porque a emenda mostrava áudio que o Short não
// conteria. Mesma coisa no início, que tocava desde (ini - 2s).
func TestNenhumaEscutaPassaDoCorte(t *testing.T) {
	js := jsDaPagina(t)

	// Nenhuma reprodução pode somar ao fim nem subtrair do início.
	for _, proibido := range []string{
		"e.fimMs / 1000 + 2", "e.iniMs / 1000 - 2", // a versão antiga, literal
		"fimMs / MS + ", "iniMs / MS - ", // qualquer reaparição da mesma ideia
	} {
		if strings.Contains(js, proibido) {
			t.Errorf("uma escuta ultrapassa o corte (%q): mostraria áudio que o Short não terá", proibido)
		}
	}
	// E o fim é passado como FUNÇÃO, para o limite acompanhar um ajuste feito durante a
	// reprodução em vez de ficar no valor capturado no clique.
	if !strings.Contains(js, "function limiteFim()") {
		t.Error("o limite de parada deveria ser reavaliado a cada tick, não capturado no clique")
	}
	for _, quer := range []string{"tocarIntervalo(efetivoAtual().iniMs / MS, limiteFim)", "tocarIntervalo(ini, limiteFim)"} {
		if !strings.Contains(js, quer) {
			t.Errorf("reprodução sem o limite dinâmico: falta %q", quer)
		}
	}
}

// TestTocaAEmendaAoAjustar é o item 3, o que derruba as 8 a 10 escutas: conferir tocando o
// trecho inteiro custa ~50s por iteração; ouvir só a ponta mexida custa ~5s.
func TestTocaAEmendaAoAjustar(t *testing.T) {
	js := jsDaPagina(t)
	if !strings.Contains(js, "REV.ultimaPonta") {
		t.Error("a tela não guarda qual ponta foi mexida, então não sabe o que tocar")
	}
	if !strings.Contains(js, "if (REV.ultimaPonta === 'inicio') ouvirInicio(); else ouvirFim();") {
		t.Error("a emenda da ponta mexida não é tocada quando o recálculo chega")
	}
	// Depois do debounce, não a cada clique.
	if !strings.Contains(js, "pedirRecalculo(i, true)") {
		t.Error("a escuta automática deveria estar atrelada ao debounce")
	}
}

// TestSemParagrafoRedundanteNoTopo é o item 4: a faixa de frases já mostra o mesmo texto, e
// melhor (destaca o que está dentro do corte). Manter os dois empurrava os controles para fora
// da tela.
func TestSemParagrafoRedundanteNoTopo(t *testing.T) {
	corpo := htmlDaRevisao(t)
	if strings.Contains(corpo, `id="texto"`) {
		t.Error("o parágrafo grande do topo voltou: duplica a faixa de frases e empurra os controles")
	}
	if !strings.Contains(corpo, `class="duas-colunas"`) {
		t.Error("sem o layout de duas colunas o operador rola muito")
	}
}

// TestFrasesComMesmoCarimboSaoAgrupadas é o item 6. A legenda tem resolução de 1 s, então duas
// frases no mesmo segundo recebem carimbos idênticos e viram duas linhas na faixa que levam ao
// MESMO tempo: o operador clica na segunda, nada muda, e soma mais um "não funcionou".
func TestFrasesComMesmoCarimboSaoAgrupadas(t *testing.T) {
	// Duas frases na mesma linha de timestamp: ambas herdam o carimbo dela.
	tr := "[00:00:00] primeira fala termina aqui. segunda fala no mesmo segundo tambem.\n" +
		"[00:00:20] terceira fala em outro segundo.\n" +
		"[00:00:40] quarta fala aqui.\n" +
		"[00:01:00] quinta fala aqui.\n"
	frases := harness.Frasear(tr)

	// Confere a pré-condição: sem isso o teste não exercita o agrupamento.
	var repetidos int
	for i := 1; i < len(frases); i++ {
		if frases[i].InicioMs == frases[i-1].InicioMs {
			repetidos++
		}
	}
	if repetidos == 0 {
		t.Skip("o Frasear não produziu carimbos repetidos nesta fixture; nada a agrupar")
	}

	got := recalcularTrecho(frases, 0, 0, 40000, ContextoAjuste{})

	// Nenhum carimbo pode aparecer duas vezes na faixa: é isso que produz o clique inócuo.
	vistos := map[int]int{}
	for _, f := range got.Vizinhanca {
		vistos[f.InicioMs]++
	}
	for ms, n := range vistos {
		if n > 1 {
			t.Errorf("o carimbo %s aparece %d vezes na faixa — clicar na segunda não mudaria nada",
				rotulo(ms), n)
		}
	}

	// O bloco agrupado mantém as duas falas no texto (nada se perde) e diz quantas são.
	for _, f := range got.Vizinhanca {
		if f.InicioMs != 0 {
			continue
		}
		if f.Falas < 2 {
			t.Errorf("o bloco de 00:00:00 deveria contar 2 falas, contou %d", f.Falas)
		}
		for _, quer := range []string{"primeira fala", "segunda fala"} {
			if !strings.Contains(f.Texto, quer) {
				t.Errorf("o agrupamento perdeu %q: %q", quer, f.Texto)
			}
		}
	}
}

// TestAgruparPorCarimboPreservaOrdemEDentro: o agrupamento não pode reordenar a faixa nem
// apagar o destaque de um bloco que toca o corte.
func TestAgruparPorCarimboPreservaOrdemEDentro(t *testing.T) {
	entrada := []FraseVizinha{
		{InicioMs: 1000, FimMs: 1000, Texto: "a", Dentro: false, Falas: 1},
		{InicioMs: 1000, FimMs: 2000, Texto: "b", Dentro: true, Falas: 1},
		{InicioMs: 3000, FimMs: 3000, Texto: "c", Dentro: true, Falas: 1},
	}
	got := agruparPorCarimbo(entrada)
	if len(got) != 2 {
		t.Fatalf("esperava 2 entradas após agrupar, veio %d", len(got))
	}
	if got[0].InicioMs != 1000 || got[1].InicioMs != 3000 {
		t.Errorf("ordem cronológica quebrada: %d, %d", got[0].InicioMs, got[1].InicioMs)
	}
	if !got[0].Dentro {
		t.Error("o bloco tem uma fala dentro do corte; deveria vir destacado (é indivisível)")
	}
	if got[0].FimMs != 2000 {
		t.Errorf("o fim do bloco deveria ser o maior das falas (2000), veio %d", got[0].FimMs)
	}
	if got[0].Falas != 2 {
		t.Errorf("contagem de falas = %d, esperado 2", got[0].Falas)
	}
	if got[0].Texto != "a b" {
		t.Errorf("texto do bloco = %q", got[0].Texto)
	}
}

// TestFaixaMostraQuantasFalasNoBloco: sem dizer, o operador vê um bloco longo e não entende por
// que não consegue começar no meio dele.
func TestFaixaMostraQuantasFalasNoBloco(t *testing.T) {
	js := jsDaPagina(t)
	if !strings.Contains(js, "f.falas > 1") {
		t.Error("a faixa não sinaliza blocos agrupados")
	}
	if !strings.Contains(js, "falas no mesmo segundo") {
		t.Error("o aviso do bloco deveria explicar o motivo em palavras")
	}
}

// --- Terceira rodada: tela simplificada ---

// TestFluxoDeDoisCliquesEhDeterministico é o item 1. A heurística do meio do trecho adivinhava
// a ponta, e quando errava o operador não tinha como entender por quê. Agora: 1º clique define
// o início, 2º o fim, 3º recomeça — e a instrução aparece ANTES do clique.
func TestFluxoDeDoisCliquesEhDeterministico(t *testing.T) {
	js := jsDaPagina(t)

	// A heurística não pode voltar: era a fonte da confusão.
	for _, proibido := range []string{"var meio = (e.iniMs + e.fimMs) / 2", "f.inicio_ms < meio"} {
		if strings.Contains(js, proibido) {
			t.Errorf("a heurística do meio do trecho voltou (%q): adivinhar a ponta confunde", proibido)
		}
	}
	// O estado é por trecho: trocar de trecho não pode herdar o passo do anterior.
	if !strings.Contains(js, "proximoClique: dados.trechos.map") {
		t.Error("o passo do fluxo deveria ser por trecho, não global")
	}
	// Os três movimentos do ciclo.
	for _, quer := range []string{
		"REV.proximoClique[i] === 'ini'", // decide pelo estado, não por posição
		"REV.proximoClique[i] = 'fim'",   // 1º clique avança para o fim
		"REV.proximoClique[i] = 'ini'",   // 2º clique recomeça
	} {
		if !strings.Contains(js, quer) {
			t.Errorf("o ciclo de dois cliques está incompleto: falta %q", quer)
		}
	}
	// A instrução tem de dizer o que vai acontecer, nos dois estados.
	for _, quer := range []string{"1. clique onde COMEÇA", "2. agora clique onde TERMINA"} {
		if !strings.Contains(js, quer) {
			t.Errorf("a instrução não cobre um dos passos: falta %q", quer)
		}
	}
	// Restaurar volta ao primeiro clique, senão o operador fica num estado que não pediu.
	// (jsDaPagina remove comentários, então a busca é pelo código puro.)
	if !strings.Contains(js, "REV.proximoClique[REV.atual] = 'ini'") {
		t.Error("restaurar deveria recomeçar o fluxo de dois cliques")
	}
}

// TestSetasEmVoltaDoValor é o item 5: « ‹ 00:04:13 › ». A direção é auto-evidente e ocupa um
// quinto do espaço dos botões rotulados.
func TestSetasEmVoltaDoValor(t *testing.T) {
	corpo := htmlDaRevisao(t)

	// Cada linha tem duas setas de cada lado do valor: dupla (1s) e simples (0,25s).
	for _, ponta := range []string{"ini", "fim"} {
		for _, suf := range []string{"mm", "m", "p", "pp"} {
			if !strings.Contains(corpo, `id="aj-`+ponta+`-`+suf+`"`) {
				t.Errorf("falta a seta aj-%s-%s", ponta, suf)
			}
		}
	}
	// Os botões rotulados saíram (ocupavam cinco vezes o espaço). Confere pelos IDS: o texto
	// "mais cedo"/"mais tarde" segue legítimo nos title das setas, como dica de acessibilidade.
	for _, naoQuer := range []string{
		`id="aj-ini-cedo"`, `id="aj-ini-tarde"`, `id="aj-fim-cedo"`, `id="aj-fim-tarde"`,
		`id="aj-usar-fim"`, `id="aj-ouvir-ini"`, `id="aj-ouvir-fim"`,
	} {
		if strings.Contains(corpo, naoQuer) {
			t.Errorf("controle antigo ainda na tela: %q", naoQuer)
		}
	}
	// As setas precisam de title: sozinhas elas são mudas para leitor de tela.
	if n := strings.Count(corpo, `title="1 segundo`) + strings.Count(corpo, `title="0,25 segundo`); n != 8 {
		t.Errorf("esperava 8 setas com title explicativo, achei %d", n)
	}
	// Uma legenda pequena explica os passos, senão as setas ficam mudas.
	if !strings.Contains(corpo, "0,25s") || !strings.Contains(corpo, "1s") {
		t.Error("falta a legenda explicando os passos das setas")
	}
	// E a ordem visual: « ‹ valor › » — mais cedo à esquerda.
	pos := func(id string) int { return strings.Index(corpo, `id="aj-`+id+`"`) }
	if !(pos("ini-mm") < pos("ini-m") && pos("ini-m") < strings.Index(corpo, `id="aj-v-ini"`) &&
		strings.Index(corpo, `id="aj-v-ini"`) < pos("ini-p") && pos("ini-p") < pos("ini-pp")) {
		t.Error("a ordem visual das setas não é « ‹ valor › »")
	}
}

// TestFaixaTemRolagemPropria é o item 4: com uma dúzia de frases, sem max-height a faixa empurra
// os controles para fora da tela — que era o problema original.
func TestFaixaTemRolagemPropria(t *testing.T) {
	pagina := htmlDaPagina(t)
	bloco := pagina[strings.Index(pagina, ".frases {"):]
	bloco = bloco[:strings.Index(bloco, "}")]
	for _, quer := range []string{"max-height", "overflow-y: auto"} {
		if !strings.Contains(bloco, quer) {
			t.Errorf("a faixa de frases precisa de %q: %s", quer, bloco)
		}
	}
	// E rola até as selecionadas ao trocar de trecho.
	js := jsDaPagina(t)
	if !strings.Contains(js, "rolarAteSelecionadas") {
		t.Error("a faixa não rola até as frases do corte")
	}
	// scrollTop, não scrollIntoView: este último rolaria a PÁGINA e desfaria o ganho.
	if strings.Contains(js, "scrollIntoView") {
		t.Error("scrollIntoView rolaria a página inteira, desfazendo o layout de uma tela")
	}
}

// TestControlesFicamNaColunaDoVideo é o item 4 do outro lado: a esquerda tinha espaço morto sob
// o player e é onde os controles cabem sem quebrar em duas linhas.
func TestControlesFicamNaColunaDoVideo(t *testing.T) {
	corpo := htmlDaRevisao(t)
	iVideo := strings.Index(corpo, `class="coluna-video"`)
	iFrases := strings.Index(corpo, `class="coluna-frases"`)
	if iVideo < 0 || iFrases < 0 {
		t.Fatal("as duas colunas deveriam existir")
	}
	if iVideo > iFrases {
		t.Error("o vídeo deveria vir antes (esquerda) e as frases depois (direita)")
	}
	// Os controles de tempo ficam na coluna do vídeo, entre ela e a das frases.
	iCtrl := strings.Index(corpo, `id="aj-v-ini"`)
	if !(iVideo < iCtrl && iCtrl < iFrases) {
		t.Error("os controles de tempo deveriam estar na coluna do vídeo, sob o player")
	}
}

// TestSeloDizQueOSomVeioDoSistema: a emenda toca sozinha; sem aviso o operador não sabe se foi o
// sistema ou um clique acidental dele.
func TestSeloDizQueOSomVeioDoSistema(t *testing.T) {
	js := jsDaPagina(t)
	if !strings.Contains(js, "avisarToque(REV.ultimaPonta)") {
		t.Error("o selo não é acionado quando a emenda toca sozinha")
	}
	corpo := htmlDaRevisao(t)
	if !strings.Contains(corpo, "tocando a emenda") {
		t.Error("falta o selo na tela")
	}
}

// TestCliqueNoFimUsaOComecoDaProximaFala: o fim do trecho é onde a próxima fala começa — é o que
// o operador quer dizer ao apontar a última frase que deve entrar.
func TestCliqueNoFimUsaOComecoDaProximaFala(t *testing.T) {
	js := jsDaPagina(t)
	if !strings.Contains(js, "function fimDaFraseSeguinte") {
		t.Error("o clique no fim deveria usar o começo da fala seguinte")
	}
	if !strings.Contains(js, "if (fs[k].inicio_ms > f.inicio_ms) return fs[k].inicio_ms") {
		t.Error("fimDaFraseSeguinte não procura a próxima fala da faixa")
	}
}

// TestFaixaNaoEhReconstruidaAoAjustar é o bug do salto de rolagem: reatribuir innerHTML zera o
// scrollTop, e a faixa (que tem rolagem própria) voltava ao topo a cada clique — no meio de um
// fluxo de dois cliques, isso quebra a sequência.
func TestFaixaNaoEhReconstruidaAoAjustar(t *testing.T) {
	js := jsDaPagina(t)

	// A chave do conjunto: só reconstrói quando as frases mudam de verdade.
	if !strings.Contains(js, "chave !== REV.faixaChave") {
		t.Error("a faixa deveria ser reconstruída só quando o conjunto de frases muda")
	}
	// O ajuste mexe apenas no destaque.
	if !strings.Contains(js, "b.classList.toggle('dentro'") {
		t.Error("o ajuste deveria alternar a classe, não recriar os elementos")
	}
	// Trocar de trecho força a reconstrução (conjunto novo). jsDaPagina remove comentários,
	// então a busca é pelo código puro — dentro do irPara.
	ip := js[strings.Index(js, "function irPara"):]
	ip = ip[:strings.Index(ip, "function decidir")]
	if !strings.Contains(ip, "REV.faixaChave = null") {
		t.Error("irPara deveria invalidar a chave, senão o trecho novo reusa a lista antiga")
	}
	// E o auto-scroll só acontece na (re)construção: no ajuste, a posição é do operador.
	corpo := js[strings.Index(js, "function desenharFrases"):]
	corpo = corpo[:strings.Index(corpo, "function rolarAteSelecionadas")]
	if strings.Count(corpo, "rolarAteSelecionadas(wrap)") != 1 {
		t.Error("rolarAteSelecionadas deveria ser chamado uma vez, dentro da reconstrução")
	}
}
