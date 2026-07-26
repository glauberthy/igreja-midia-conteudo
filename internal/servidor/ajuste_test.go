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
	got := recalcularTrecho(frases, 0, 36, 78, LimitesPregacao{})
	if !got.Aprovavel {
		t.Fatalf("deveria ser aprovável: %s", got.Motivo)
	}
	if got.DuracaoSeg != 42 {
		t.Errorf("duração = %.1f, queria 42", got.DuracaoSeg)
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

	original := recalcularTrecho(frases, 0, 60, 102, LimitesPregacao{}) // 1:00 → 1:42
	estendido := recalcularTrecho(frases, 0, 36, 78, LimitesPregacao{}) // recuou 24 s

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

	for _, marcado := range []float64{36, 36.4, 37.9, 35.2, 38.7} {
		got := recalcularTrecho(frases, 0, marcado, marcado+42, LimitesPregacao{})
		if got.Hook == "" {
			t.Fatalf("marcado %.1f: sem hook", marcado)
		}
		idx, achou := harness.AcharAncora(frases, got.Hook)
		if !achou {
			t.Fatalf("marcado %.1f: hook não encontrado na transcrição", marcado)
		}
		startMs, ok := validacao.HmsToMs(got.Start)
		if !ok {
			t.Fatalf("marcado %.1f: start ilegível %q", marcado, got.Start)
		}
		if delta := frases[idx].InicioMs - startMs; delta != 0 {
			t.Errorf("marcado %.1f: Δ=%dms entre hook e start — o auditor acusaria", marcado, delta)
		}
	}
}

// TestEncaixeEmFronteiraDeFala documenta e trava o encaixe: o operador não precisa de
// precisão, e a resposta diz que houve encaixe (para a tela poder mostrar onde caiu).
func TestEncaixeEmFronteiraDeFala(t *testing.T) {
	frases := frasesAjuste(t)

	got := recalcularTrecho(frases, 0, 37.4, 79.2, LimitesPregacao{})
	if got.StartSeg != 36 {
		t.Errorf("start não encaixou na fronteira: %.2f, queria 36", got.StartSeg)
	}
	if !got.AjustadoStart {
		t.Error("AjustadoStart deveria avisar que o ponto foi movido")
	}
	// Fim de frase completa: a frase que começa em 1:12 termina no início da seguinte.
	if got.EndSeg == 79.2 {
		t.Error("end não encaixou em fim de frase completa")
	}
}

// TestGuardaDuracaoMinima e a máxima: a mensagem tem de trazer os números, não "fora da
// faixa". É o que permite ao operador consertar sem adivinhar.
func TestGuardaDuracaoForaDaFaixa(t *testing.T) {
	frases := frasesAjuste(t)

	curto := recalcularTrecho(frases, 0, 36, 54, LimitesPregacao{}) // 18 s
	if curto.Aprovavel {
		t.Error("18 s deveria ser recusado (mínimo 30 s)")
	}
	if !strings.Contains(curto.Motivo, "mínimo é 30") || !strings.Contains(curto.Motivo, "estenda") {
		t.Errorf("motivo não diz o que falta: %q", curto.Motivo)
	}

	longo := recalcularTrecho(frases, 0, 0, 66, LimitesPregacao{}) // 66 s
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
	for _, c := range []struct{ ini, fim float64 }{{60, 60}, {60, 30}, {60, 59.9}} {
		got := recalcularTrecho(frases, 0, c.ini, c.fim, LimitesPregacao{})
		if got.Aprovavel {
			t.Errorf("start=%.1f end=%.1f deveria ser recusado", c.ini, c.fim)
		}
		if got.Motivo == "" {
			t.Errorf("start=%.1f end=%.1f: recusado sem explicar", c.ini, c.fim)
		}
	}
}

// TestClampNosLimitesDaPregacao: o ajuste não escapa da janela informada no pedido — fora
// dela está o louvor e os avisos, que o recorte existe para excluir.
func TestClampNosLimitesDaPregacao(t *testing.T) {
	frases := frasesAjuste(t)
	lim := LimitesPregacao{IniMs: 36000, FimMs: 120000} // 0:36 → 2:00

	got := recalcularTrecho(frases, 0, 0, 300, lim) // tentou 0:00 → 5:00
	if got.StartSeg < 36 {
		t.Errorf("start escapou do limite inferior: %.1f", got.StartSeg)
	}
	if got.EndSeg > 120 {
		t.Errorf("end escapou do limite superior: %.1f", got.EndSeg)
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

func postAjustar(t *testing.T, s *Servidor, indice int, ini, fim float64) (int, TrechoAjustado) {
	t.Helper()
	corpo, _ := json.Marshal(map[string]any{"indice": indice, "start": ini, "end": fim})
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

	code, got := postAjustar(t, s, 0, 36, 78)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if got.DuracaoSeg != 42 {
		t.Errorf("duração = %.1f, queria 42", got.DuracaoSeg)
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
	if code, _ := postAjustar(t, s, 99, 36, 78); code != http.StatusBadRequest {
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
		"ajustes":   []map[string]any{{"indice": 0, "start": 36, "end": 78}},
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
		"ajustes":   []map[string]any{{"indice": 0, "start": 0, "end": 66}},
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
		"ajustes": []map[string]any{{"indice": 1, "start": 0, "end": 500}},
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
