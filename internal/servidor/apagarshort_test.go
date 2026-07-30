package servidor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"srtclean/internal/pipeline"
)

// A tela de resultado (spec-05 v3, Parte 4) apaga arquivo. Endpoint que apaga com travessia de
// caminho é muito pior que um que baixa — daí a whitelist ser a MESMA do download, num lugar só.

// pedidoConcluido roda o ciclo até o fim e devolve o id, com os Shorts em disco.
func pedidoConcluido(t *testing.T, s *Servidor) string {
	t.Helper()
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, "teste-1", []int{0, 1})
	esperarStatus(t, s, "teste-1", pipeline.EstadoConcluido)
	return "teste-1"
}

func apagarShort(t *testing.T, s *Servidor, id, arquivo string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("DELETE", "/finalizados/"+id+"/"+arquivo, nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

// TestApagarShortRemoveOArquivoESaiDaLista: o caminho normal.
func TestApagarShortRemoveOArquivoESaiDaLista(t *testing.T) {
	s := servidorPesada(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})
	id := pedidoConcluido(t, s)

	s.mu.Lock()
	nomes := append([]string(nil), s.pedidos[id].shorts...)
	s.mu.Unlock()
	if len(nomes) < 2 {
		t.Fatalf("o ciclo deveria ter gerado 2 Shorts, gerou %d", len(nomes))
	}
	alvo := nomes[0]
	caminho := filepath.Join(s.outDir, id, alvo)
	if _, err := os.Stat(caminho); err != nil {
		t.Fatalf("o Short não está em disco antes de apagar: %v", err)
	}

	rec := apagarShort(t, s, id, alvo)
	if rec.Code != http.StatusOK {
		t.Fatalf("apagar devolveu %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(caminho); !os.IsNotExist(err) {
		t.Errorf("o arquivo continua em disco depois do DELETE (%v)", err)
	}

	// A resposta é o estado do pedido JÁ sem o arquivo: quem manda na lista é o servidor.
	var st statusJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("resposta não é o estado do pedido: %v (%s)", err, rec.Body.String())
	}
	for _, sh := range st.Shorts {
		if sh.Nome == alvo {
			t.Errorf("o Short apagado continua na lista devolvida: %+v", st.Shorts)
		}
	}
	if len(st.Shorts) != len(nomes)-1 {
		t.Errorf("a lista tem %d Short(s), esperava %d", len(st.Shorts), len(nomes)-1)
	}
	// O OUTRO Short não foi tocado — apagar é por arquivo, não por pedido.
	if _, err := os.Stat(filepath.Join(s.outDir, id, nomes[1])); err != nil {
		t.Errorf("o outro Short do pedido desapareceu: %v", err)
	}
}

// TestApagarShortRecusaForaDaWhitelist é o par do TestBaixarFinalRecusaArquivoForaDaWhitelist, e
// existe porque a consequência aqui é destrutiva: um nome vindo da URL que virasse caminho
// apagaria arquivo do operador fora de finalizados/.
func TestApagarShortRecusaForaDaWhitelist(t *testing.T) {
	s := servidorPesada(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})
	id := pedidoConcluido(t, s)

	// Um arquivo real, na pasta do pedido, que o render NÃO gerou: alvo perfeito para travessia.
	vitima := filepath.Join(s.outDir, id, "nao-mexa.txt")
	if err := os.WriteFile(vitima, []byte("conteúdo do operador"), 0644); err != nil {
		t.Fatal(err)
	}

	for _, nome := range []string{
		"nao-mexa.txt",           // existe, mas não está na whitelist
		"..%2f..%2fetc%2fpasswd", // travessia escapada
		"short_99.mp4",           // nome plausível que este pedido não gerou
	} {
		rec := apagarShort(t, s, id, nome)
		if rec.Code != http.StatusNotFound {
			t.Errorf("DELETE de %q deveria dar 404, veio %d", nome, rec.Code)
		}
	}
	if _, err := os.Stat(vitima); err != nil {
		t.Errorf("o arquivo fora da whitelist foi apagado: %v", err)
	}
}

// TestApagarDuasVezesNaoErra: o operador clica duas vezes, ou tem duas abas. Depois do primeiro
// DELETE o nome saiu da whitelist, então o segundo é 404 — e nada além disso.
func TestApagarDuasVezesNaoErra(t *testing.T) {
	s := servidorPesada(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})
	id := pedidoConcluido(t, s)
	s.mu.Lock()
	alvo := s.pedidos[id].shorts[0]
	s.mu.Unlock()

	if rec := apagarShort(t, s, id, alvo); rec.Code != http.StatusOK {
		t.Fatalf("primeiro DELETE devolveu %d", rec.Code)
	}
	if rec := apagarShort(t, s, id, alvo); rec.Code != http.StatusNotFound {
		t.Errorf("segundo DELETE devolveu %d, quero 404", rec.Code)
	}
	// E o download do mesmo nome também para de funcionar: uma whitelist só serve as duas rotas.
	req := httptest.NewRequest("GET", "/finalizados/"+id+"/"+alvo, nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("baixar um Short apagado devolveu %d, quero 404", rec.Code)
	}
}

// TestApagarNaoTocaNoCortesCSV: decisão do dono — apagar o arquivo NÃO apaga a medição. O
// cortes.csv é dado de pesquisa sobre o desvio da legenda; um Short apagado não desfaz o fato de
// aquele corte ter sido aprovado naqueles tempos.
func TestApagarNaoTocaNoCortesCSV(t *testing.T) {
	s := servidorPesada(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})
	id := pedidoConcluido(t, s)
	esperarArquivo(t, s.cortesPath)
	antes, err := os.ReadFile(s.cortesPath)
	if err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	alvo := s.pedidos[id].shorts[0]
	s.mu.Unlock()
	if rec := apagarShort(t, s, id, alvo); rec.Code != http.StatusOK {
		t.Fatalf("DELETE devolveu %d", rec.Code)
	}

	depois, err := os.ReadFile(s.cortesPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(antes) != string(depois) {
		t.Error("o cortes.csv mudou ao apagar um Short: medição não é inventário")
	}
}

// TestEstadoTrazTamanhoDeCadaShort: a tela avisa o operador quando o arquivo passa do limite do
// WhatsApp, e para isso o peso tem de vir do servidor (o cliente não tem como saber sem baixar).
func TestEstadoTrazTamanhoDeCadaShort(t *testing.T) {
	s := servidorPesada(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})
	id := pedidoConcluido(t, s)

	req := httptest.NewRequest("GET", "/pedidos/"+id, nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	var st statusJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("estado ilegível: %v (%s)", err, rec.Body.String())
	}
	if len(st.Shorts) == 0 {
		t.Fatal("o estado não trouxe Short nenhum")
	}
	for _, sh := range st.Shorts {
		if sh.Nome == "" {
			t.Error("Short sem nome no estado")
		}
		if sh.Bytes <= 0 {
			t.Errorf("Short %q veio com bytes=%d: a tela não conseguiria avisar sobre o limite "+
				"do WhatsApp", sh.Nome, sh.Bytes)
		}
	}
}
