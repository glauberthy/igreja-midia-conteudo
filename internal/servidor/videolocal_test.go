package servidor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"srtclean/internal/pipeline"
	"srtclean/internal/video"
	"srtclean/internal/videocache"
)

// FATIA 2 da spec-05 v4: o preview passa a usar o MESMO arquivo que o corte.
//
// O que isto elimina por construção: o operador escolhia o ponto ouvindo o player do YouTube e o
// corte acontecia no arquivo baixado — duas fontes, dois relógios, e um overshoot de +89 ms
// medido na parada. Servindo o arquivo local, não há o que compensar.

func pedirVideo(t *testing.T, s *Servidor, id string, faixa string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/video/"+id, nil)
	if faixa != "" {
		req.Header.Set("Range", faixa)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

// TestRotaDoVideoServeComRange é o requisito que o dono pediu para confirmar: sem range, o
// navegador teria de baixar 902 MB para dar seek em 01:30:00.
func TestRotaDoVideoServeComRange(t *testing.T) {
	s := servidorPesada(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})
	dir, err := s.cache.DirVideo("cultoTeste1")
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(dir, 0755)
	if err := escreverVideoFalso(filepath.Join(dir, videocache.NomeVideo)); err != nil {
		t.Fatal(err)
	}

	rec := pedirVideo(t, s, "cultoTeste1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /video devolveu %d", rec.Code)
	}
	if got := rec.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Errorf("Accept-Ranges = %q, quero \"bytes\" — sem isso o <video> não faz seek sem "+
			"baixar o culto inteiro", got)
	}

	// Uma faixa no meio: tem de vir 206 com Content-Range, e SÓ os bytes pedidos.
	rec = pedirVideo(t, s, "cultoTeste1", "bytes=1000000-1000099")
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("pedido com Range devolveu %d, quero 206", rec.Code)
	}
	if got := rec.Header().Get("Content-Range"); !strings.HasPrefix(got, "bytes 1000000-1000099/") {
		t.Errorf("Content-Range = %q", got)
	}
	if n := rec.Body.Len(); n != 100 {
		t.Errorf("transferiu %d bytes para uma faixa de 100", n)
	}
}

// TestRotaDoVideoRecusaOQueNaoServe: o id vem da URL e vira nome de diretório.
func TestRotaDoVideoRecusaOQueNaoServe(t *testing.T) {
	s := servidorPesada(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})
	// Um arquivo fora do cache, para a travessia ter alvo real.
	fora := filepath.Join(t.TempDir(), "segredo.mp4")
	os.WriteFile(fora, []byte("nao e video de culto"), 0644)

	for _, id := range []string{
		"cultoInexistente", // não está no cache
		"..",               // travessia
		"",                 // vazio
		"nao-existe-mesmo", // id plausível que não está em disco
	} {
		rec := pedirVideo(t, s, id, "")
		// 404 do handler ou 3xx do ServeMux (que normaliza o caminho ANTES de rotear, então
		// "/video/.." nem chega aqui). O que não pode é 200 com bytes: são duas guardas em
		// série, e o teste aceita qualquer uma barrando.
		if rec.Code == http.StatusOK || rec.Code == http.StatusPartialContent {
			t.Errorf("GET /video/%q SERVIU conteúdo (código %d, %d bytes)", id, rec.Code, rec.Body.Len())
		}
	}
	if _, err := os.Stat(fora); err != nil {
		t.Error("o arquivo de fora do cache foi tocado")
	}
}

// TestVideoParcialNaoEServido: um download interrompido deixa mp4 truncado no cache. Servi-lo
// seria dar ao operador um preview que não é o produto — e TemVideo existe para isso.
func TestVideoParcialNaoEServido(t *testing.T) {
	s := servidorPesada(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})
	dir, _ := s.cache.DirVideo("cultoTeste1")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, videocache.NomeVideo), make([]byte, 1024), 0644) // 1 KB

	if rec := pedirVideo(t, s, "cultoTeste1", ""); rec.Code != http.StatusNotFound {
		t.Errorf("um vídeo de 1 KB foi servido como culto (código %d)", rec.Code)
	}
}

// TestRevisaoDizSeHaVideoLocal: o cliente precisa saber se pode tocar. Sem o campo ele apontaria
// o <video> para uma rota 404 e mostraria um player quebrado, sem explicação.
func TestRevisaoDizSeHaVideoLocal(t *testing.T) {
	bv := &baixadorVideoFake{}
	s := servidorComCache(t, candsJanela(), bv, &renderFake{})
	id := criarPedido(t, s, "https://youtu.be/cultoTeste1", "00:00:00", "00:10:00")
	esperarStatus(t, s, id, pipeline.EstadoAguardandoAprovacao)

	req := httptest.NewRequest("GET", "/pedidos/"+id, nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	// O payload da revisão vive no fragmento HTML; aqui basta o dado do JSON de dados-trechos.
	s.mu.Lock()
	reg := s.pedidos[id]
	dados := revisaoJSON(reg, s.temVideoLocal(reg))
	s.mu.Unlock()
	var d struct {
		VideoLocal bool `json:"videoLocal"`
	}
	if err := json.Unmarshal([]byte(dados), &d); err != nil {
		t.Fatal(err)
	}
	if !d.VideoLocal {
		t.Error("videoLocal = false com o vídeo em cache: a tela não vai oferecer escuta")
	}
}

// TestFaseLevePreparaOVideoAntesDaRevisao é o coração da fatia: quando o operador VÊ os trechos, o
// vídeo já tem de estar em disco — é dele que saem as pausas (fronteira do corte) e a escuta.
func TestFaseLevePreparaOVideoAntesDaRevisao(t *testing.T) {
	bv := &baixadorVideoFake{}
	s := servidorPesada(t, candsJanela(), bv, &renderFake{})
	s.analisadorPausas = &analisadorFake{pausas: []video.Pausa{{InicioMs: 30000, FimMs: 30600}}}
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)

	bv.mu.Lock()
	chamado := bv.chamado
	bv.mu.Unlock()
	if !chamado {
		t.Error("a fase leve não baixou o vídeo: a revisão começaria sem escuta e sem pausas")
	}
	if !s.cache.TemVideo("cultoTeste1") {
		t.Error("o vídeo não está no cache quando a revisão abre")
	}
	if _, err := s.cache.LerPausas("cultoTeste1"); err != nil {
		t.Errorf("as pausas não foram geradas antes da revisão: %v", err)
	}
	// E o tempo de download foi MEDIDO na fase leve (a coluna baixar_video_s do tempos.csv).
	s.mu.Lock()
	m := s.pedidos["teste-1"].metricas
	s.mu.Unlock()
	if m == nil {
		t.Fatal("métricas ausentes")
	}
}

// TestDownloadQueFalhaNaFaseLeveNaoImpedeARevisao: evidência da regra "melhor revisar com a
// ferramenta antiga que não revisar". E o resíduo NÃO pode ficar no cache.
func TestDownloadQueFalhaNaFaseLeveNaoImpedeARevisao(t *testing.T) {
	bv := &baixadorVideoFake{erro: fmt.Errorf("googlevideo timeout")}
	s := servidorPesada(t, candsJanela(), bv, &renderFake{})
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)

	if s.cache.TemVideo("cultoTeste1") {
		t.Error("um download que falhou deixou vídeo utilizável no cache")
	}
	s.mu.Lock()
	reg := s.pedidos["teste-1"]
	temLocal := s.temVideoLocal(reg)
	s.mu.Unlock()
	if temLocal {
		t.Error("a tela vai oferecer escuta de um vídeo que não existe")
	}
}

// TestParcialDoDownloadSaiDoCache: o furo é caro — um mp4 truncado acima de 20 MB faria o cache
// dizer "tenho vídeo" e o corte sair de um arquivo incompleto.
func TestParcialDoDownloadSaiDoCache(t *testing.T) {
	s := servidorPesada(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})
	dir, _ := s.cache.DirVideo("cultoTeste1")
	os.MkdirAll(dir, 0755)
	alvo := filepath.Join(dir, videocache.NomeVideo)
	if err := escreverVideoFalso(alvo); err != nil { // 20 MB, SEM video.json
		t.Fatal(err)
	}

	s.limparVideoParcial("cultoTeste1", dir)
	if _, err := os.Stat(alvo); !os.IsNotExist(err) {
		t.Error("o parcial sem registro continuou no cache")
	}

	// Agora COM registro: um download anterior que funcionou não pode ser apagado por uma falha
	// posterior.
	if err := escreverVideoFalso(alvo); err != nil {
		t.Fatal(err)
	}
	if err := s.cache.Registrar("cultoTeste1", 0, "Culto"); err != nil {
		t.Fatal(err)
	}
	s.limparVideoParcial("cultoTeste1", dir)
	if _, err := os.Stat(alvo); err != nil {
		t.Error("apagou um vídeo REGISTRADO por causa de uma falha posterior")
	}
}
