package servidor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"srtclean/internal/pipeline"
	"srtclean/internal/validacao"
)

// tresCandidatos devolve um selecionadorFake com 3 candidatos (índices 0,1,2).
func tresCandidatos() *selecionadorFake {
	return &selecionadorFake{cands: []validacao.Candidato{
		{Hook: "Trecho 0", Start: "00:00:10", End: "00:00:40", DurationSeconds: 30, Score: 80},
		{Hook: "Trecho 1", Start: "00:01:00", End: "00:01:35", DurationSeconds: 35, Score: 85},
		{Hook: "Trecho 2", Start: "00:02:00", End: "00:02:34", DurationSeconds: 34, Score: 77},
	}}
}

// prontoParaAprovar cria um pedido e espera ele chegar em aguardando-aprovacao.
func prontoParaAprovar(t *testing.T, sel Selecionador) *Servidor {
	t.Helper()
	s := servidorTeste(t, &baixadorFake{transc: "x"}, sel)
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)
	return s
}

func aprovarJSON(t *testing.T, s *Servidor, id string, indices []int) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(map[string][]int{"aprovados": indices})
	req := httptest.NewRequest("POST", "/pedidos/"+id+"/aprovar", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func TestAprovarJSONMudaEstadoERegistraIndices(t *testing.T) {
	s := prontoParaAprovar(t, tresCandidatos())

	rec := aprovarJSON(t, s, "teste-1", []int{0, 2})
	if rec.Code != http.StatusOK {
		t.Fatalf("código = %d, quero 200 (corpo %q)", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID        string `json:"id"`
		Status    string `json:"status"`
		Aprovados []int  `json:"aprovados"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json inválido: %v", err)
	}
	if resp.Status != string(pipeline.EstadoAguardandoProcessamento) {
		t.Errorf("status = %q, quero aguardando-processamento", resp.Status)
	}
	if len(resp.Aprovados) != 2 || resp.Aprovados[0] != 0 || resp.Aprovados[1] != 2 {
		t.Errorf("aprovados = %v, quero [0 2]", resp.Aprovados)
	}

	// Estado persistido no registro em memória (para a fase pesada da Parte 3).
	s.mu.Lock()
	reg := s.pedidos["teste-1"]
	st, aps := reg.ped.Status, reg.aprovados
	s.mu.Unlock()
	if st != pipeline.EstadoAguardandoProcessamento {
		t.Errorf("registro status = %q", st)
	}
	if len(aps) != 2 || aps[0] != 0 || aps[1] != 2 {
		t.Errorf("registro aprovados = %v, quero [0 2]", aps)
	}
}

func TestAprovarFormHTMXRetomaPolling(t *testing.T) {
	// Sem deps da fase pesada (baixadorVideo/renderizador nil), aprovar apenas registra e
	// deixa o pedido em aguardando-processamento — o fragmento HTML volta a fazer polling
	// (a página vai acompanhar a fase pesada quando ela existir).
	s := prontoParaAprovar(t, tresCandidatos())

	// Como o HTMX serializa checkboxes: aprovados=0&aprovados=1.
	form := url.Values{"aprovados": {"0", "1"}}
	req := httptest.NewRequest("POST", "/pedidos/teste-1/aprovar", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("código = %d, quero 200", rec.Code)
	}
	corpo := rec.Body.String()
	if !strings.Contains(corpo, `hx-trigger="every 2s"`) {
		t.Errorf("fragmento pós-aprovação deveria retomar o polling: %q", corpo)
	}
	s.mu.Lock()
	st := s.pedidos["teste-1"].ped.Status
	s.mu.Unlock()
	if st != pipeline.EstadoAguardandoProcessamento {
		t.Errorf("estado = %q, quero aguardando-processamento", st)
	}
}

func TestAprovarIndiceForaDoIntervalo(t *testing.T) {
	s := prontoParaAprovar(t, tresCandidatos())
	rec := aprovarJSON(t, s, "teste-1", []int{5}) // só há 0,1,2
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("código = %d, quero 400", rec.Code)
	}
	// Estado não muda com entrada inválida.
	s.mu.Lock()
	st := s.pedidos["teste-1"].ped.Status
	s.mu.Unlock()
	if st != pipeline.EstadoAguardandoAprovacao {
		t.Errorf("estado mudou apesar do índice inválido: %q", st)
	}
}

func TestAprovarDeduplicaIndices(t *testing.T) {
	s := prontoParaAprovar(t, tresCandidatos())
	rec := aprovarJSON(t, s, "teste-1", []int{1, 1, 0, 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("código = %d, quero 200", rec.Code)
	}
	s.mu.Lock()
	aps := s.pedidos["teste-1"].aprovados
	s.mu.Unlock()
	if len(aps) != 2 || aps[0] != 1 || aps[1] != 0 {
		t.Errorf("aprovados = %v, quero [1 0] (dedup preservando ordem)", aps)
	}
}

func TestAprovarListaVaziaReprovaTudo(t *testing.T) {
	s := prontoParaAprovar(t, tresCandidatos())
	rec := aprovarJSON(t, s, "teste-1", []int{})
	if rec.Code != http.StatusOK {
		t.Fatalf("código = %d, quero 200 (reprovar tudo é válido)", rec.Code)
	}
	s.mu.Lock()
	st := s.pedidos["teste-1"].ped.Status
	aps := s.pedidos["teste-1"].aprovados
	s.mu.Unlock()
	if st != pipeline.EstadoAguardandoProcessamento {
		t.Errorf("estado = %q, quero aguardando-processamento", st)
	}
	if len(aps) != 0 {
		t.Errorf("aprovados = %v, quero vazio", aps)
	}
}

func TestAprovarAntesDaSelecaoConcluir(t *testing.T) {
	// Trava o download para o pedido ficar em progresso; aprovar deve dar 409.
	liberar := make(chan struct{})
	b := &baixadorFake{transc: "x", liberar: liberar}
	s := servidorTeste(t, b, tresCandidatos())
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoBaixandoLegenda)

	rec := aprovarJSON(t, s, "teste-1", []int{0})
	if rec.Code != http.StatusConflict {
		t.Fatalf("código = %d, quero 409 (seleção não terminou)", rec.Code)
	}

	close(liberar) // libera a goroutine e deixa terminar antes do cleanup
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)
}

func TestAprovarPedidoInexistente404(t *testing.T) {
	s := servidorTeste(t, &baixadorFake{}, &selecionadorFake{})
	rec := aprovarJSON(t, s, "nao-existe", []int{0})
	if rec.Code != http.StatusNotFound {
		t.Errorf("código = %d, quero 404", rec.Code)
	}
}
