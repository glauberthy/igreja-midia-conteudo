package servidor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	got := recalcularTrecho(frases, 0, 36000, 78000, LimitesPregacao{})
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

	original := recalcularTrecho(frases, 0, 60000, 102000, LimitesPregacao{}) // 1:00 → 1:42
	estendido := recalcularTrecho(frases, 0, 36000, 78000, LimitesPregacao{}) // recuou 24 s

	if original.Hook == estendido.Hook {
		t.Fatal("o hook não mudou ao estender para trás — a regra da Fase 3 não foi aplicada")
	}
	if !strings.Contains(estendido.Hook, "frase numero 6") {
		t.Errorf("hook novo não é a primeira frase da janela nova: %q", estendido.Hook)
	}
}

// TestInvarianteDoAuditorSeMantem: o hook tem de começar EXATAMENTE no start (Δ=0), que é o
// que cmd/auditar verifica. Vale para tempos "tortos" marcados no player.
func TestInvarianteDoAuditorSeMantem(t *testing.T) {
	frases := frasesAjuste(t)

	// Tempos "tortos" em ms, como o player entregaria.
	for _, marcado := range []int{36000, 36400, 37900, 35200, 38700} {
		got := recalcularTrecho(frases, 0, marcado, marcado+42000, LimitesPregacao{})
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
		if delta := frases[idx].InicioMs - startMs; delta != 0 {
			t.Errorf("marcado %dms: Δ=%dms entre hook e start — o auditor acusaria", marcado, delta)
		}
	}
}

// TestEncaixeEmFronteiraDeFala documenta e trava o encaixe: o operador não precisa de
// precisão, e a resposta diz que houve encaixe (para a tela poder mostrar onde caiu).
func TestEncaixeEmFronteiraDeFala(t *testing.T) {
	frases := frasesAjuste(t)

	got := recalcularTrecho(frases, 0, 37400, 79200, LimitesPregacao{})
	if got.StartMs != 36000 {
		t.Errorf("start não encaixou na fronteira: %dms, queria 36000", got.StartMs)
	}
	if !got.AjustadoStart {
		t.Error("AjustadoStart deveria avisar que o ponto foi movido")
	}
}

// TestFimLiberadoParaFrente é o caso que motivou o ajuste manual, e o que um encaixe
// simétrico anularia: o timestamp da legenda adianta o áudio, o operador ouve a palavra
// engolida e estende 2 s. Se o sistema o devolvesse à fronteira, o ajuste não serviria
// para nada — ele marca +2s e o sistema desfaz.
func TestFimLiberadoParaFrente(t *testing.T) {
	frases := frasesAjuste(t)

	// Fronteiras de 6 em 6 s. O operador marcou 2 s depois de uma delas.
	got := recalcularTrecho(frases, 0, 36000, 80000, LimitesPregacao{})
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

// TestFimAntesDaFronteiraEncaixaParaFrente: para trás cortaria fala no meio, que é
// exatamente o defeito. Então o encaixe só existe nessa direção.
func TestFimAntesDaFronteiraEncaixaParaFrente(t *testing.T) {
	frases := frasesAjuste(t)

	// Antes da primeira fronteira (a primeira frase termina em 6 s).
	got := recalcularTrecho(frases, 0, 0, 3000, LimitesPregacao{})
	if got.EndMs < 3000 {
		t.Errorf("o end foi para trás (%dms) — cortaria fala no meio", got.EndMs)
	}
}

// TestFolgaDoFimTemTeto: folga é para sincronia de legenda, não licença para vazar. Além do
// teto, o sistema limita — senão o operador estica sem perceber e o auditor acusa depois.
func TestFolgaDoFimTemTeto(t *testing.T) {
	frases := frasesAjuste(t)

	// Última fronteira é 174 s (frase 29 começa em 174). Marcar 200 s pede 26 s de folga.
	got := recalcularTrecho(frases, 0, 150000, 200000, LimitesPregacao{})
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
	got := recalcularTrecho(frases, 0, 36000, 80000, LimitesPregacao{}) // 2 s de folga no fim
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

	curto := recalcularTrecho(frases, 0, 36000, 54000, LimitesPregacao{}) // 18 s
	if curto.Aprovavel {
		t.Error("18 s deveria ser recusado (mínimo 30 s)")
	}
	if !strings.Contains(curto.Motivo, "mínimo é 30") || !strings.Contains(curto.Motivo, "estenda") {
		t.Errorf("motivo não diz o que falta: %q", curto.Motivo)
	}

	longo := recalcularTrecho(frases, 0, 0, 66000, LimitesPregacao{}) // 66 s
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
		got := recalcularTrecho(frases, 0, c.ini, c.fim, LimitesPregacao{})
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

	got := recalcularTrecho(frases, 0, 0, 300000, lim) // tentou 0:00 → 5:00
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

func htmlDaRevisao(t *testing.T) string {
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
		`id="ajuste"`, `id="aj-frases"`, // a faixa de frases clicável
		`id="aj-v-ini"`, `id="aj-v-fim"`, // o valor ENTRE os botões que o mudam
		`id="aj-ini-cedo"`, `id="aj-ini-tarde"`, `id="aj-fim-cedo"`, `id="aj-fim-tarde"`,
		`id="aj-usar-ini"`, `id="aj-usar-fim"`, // "usar <tempo> do player"
		`id="aj-ouvir-fim"`, // ouvir o fim, junto dos controles do fim
		`id="aj-restaurar"`, `id="aj-resumo"`, `id="aj-invalido"`, `id="btn-meia"`,
	} {
		if !strings.Contains(corpo, quer) {
			t.Errorf("o fragmento de revisão não trouxe %q", quer)
		}
	}
}

// TestRotulosPeloEfeito trava a correção do principal ponto de confusão: "−1s" e "+1s" não
// diziam o que faziam — "−1s" no início deixa o trecho MAIS longo, e "+1s" no fim também, o
// que fazia o mesmo rótulo produzir efeitos opostos por linha.
func TestRotulosPeloEfeito(t *testing.T) {
	corpo := htmlDaRevisao(t)
	for _, quer := range []string{"mais cedo", "mais tarde"} {
		if !strings.Contains(corpo, quer) {
			t.Errorf("os controles precisam ser rotulados pelo efeito: falta %q", quer)
		}
	}
	// Os rótulos por sinal não podem voltar nos botões de 1s (o ajuste fino de 0,25s no fim
	// segue com sinal, e ali é adequado: é um passo fino, subordinado e explícito).
	for _, naoQuer := range []string{">−1s<", ">+1s<"} {
		if strings.Contains(corpo, naoQuer) {
			t.Errorf("rótulo por sinal voltou (%s): exige tradução mental a cada clique", naoQuer)
		}
	}
	// "Marcar aqui" não dizia AQUI ONDE. O botão agora nomeia o tempo do player.
	if strings.Contains(corpo, "Marcar aqui") || strings.Contains(corpo, "⤓ Marcar") {
		t.Error(`"Marcar aqui" voltou: o operador não vê que tempo está capturando`)
	}
	if !strings.Contains(corpo, "do player") {
		t.Error("o botão precisa nomear o tempo do player")
	}
}

// TestAjusteFinoSoNoFimESubordinado: a assimetria (Fim com passo fino, Início sem) parecia
// defeito. Como linha subordinada e rotulada "ajuste fino", lê como recurso extra do fim.
func TestAjusteFinoSoNoFimESubordinado(t *testing.T) {
	corpo := htmlDaRevisao(t)
	if !strings.Contains(corpo, "ajuste fino") {
		t.Error("o passo fino precisa estar rotulado como subordinado")
	}
	for _, quer := range []string{`id="aj-fim-m025"`, `id="aj-fim-p025"`} {
		if !strings.Contains(corpo, quer) {
			t.Errorf("o fim precisa do passo fino: falta %q", quer)
		}
	}
	for _, naoQuer := range []string{`id="aj-ini-m025"`, `id="aj-ini-p025"`} {
		if strings.Contains(corpo, naoQuer) {
			t.Errorf("o início NÃO deve ter passo de 0,25s (%s): o encaixe de 1s o torna inócuo", naoQuer)
		}
	}
}

// TestSemFalsaPrecisaoNaTela: "54.75s" e "00:40:12.000" anunciam precisão que o sistema não
// tem — a transcrição vem em segundos inteiros. Mesma falsa precisão já rejeitada na grade.
func TestSemFalsaPrecisaoNaTela(t *testing.T) {
	frases := frasesAjuste(t)
	got := recalcularTrecho(frases, 0, 36000, 80000, LimitesPregacao{})

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
	curto := recalcularTrecho(frases, 0, 36000, 54000, LimitesPregacao{})
	if strings.Contains(curto.Motivo, ".") {
		t.Errorf("mensagem com falsa precisão: %q", curto.Motivo)
	}
}

// TestVizinhancaMarcaDentroEFora é o contrato da faixa clicável: as frases do corte vêm
// marcadas, e há contexto dos dois lados para o operador apontar onde a ideia começa/termina.
func TestVizinhancaMarcaDentroEFora(t *testing.T) {
	frases := frasesAjuste(t)
	got := recalcularTrecho(frases, 0, 36000, 78000, LimitesPregacao{})

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
		got := recalcularTrecho(frases, 0, c.ini, c.fim, LimitesPregacao{})
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

// TestSeekToSegueARecomendacaoDoYouTube: allowSeekAhead=false durante os empurrões (não
// dispara requisição de vídeo a cada clique) e true quando o debounce assenta. É recomendação
// da documentação do player, e aqui é o que evita uma rajada de requisições.
func TestSeekToSegueARecomendacaoDoYouTube(t *testing.T) {
	js := htmlDaPagina(t)
	if !strings.Contains(js, "seekTo(iniMs / MS, false)") {
		t.Error("o empurrão deveria usar allowSeekAhead=false (barato, só reposiciona)")
	}
	if !strings.Contains(js, "seekTo(resp.start_ms / MS, true)") {
		t.Error("ao assentar o debounce deveria usar allowSeekAhead=true (busca de fato)")
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
		got := recalcularTrecho(frases, 0, ini, fim+250, LimitesPregacao{})
		ini, fim = got.StartMs, got.EndMs
	}
	if fim != 80000 {
		t.Errorf("após 8 empurrões de 250ms o fim é %dms, esperado exatamente 80000", fim)
	}
}
