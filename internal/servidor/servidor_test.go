package servidor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"srtclean/internal/download"
	"srtclean/internal/pipeline"
	"srtclean/internal/validacao"
)

// --- Mocks das dependências (não sobem yt-dlp nem o modelo) ---

// baixadorFake simula a fase de download da legenda. Escreve a transcrição (se sucesso)
// e pode devolver um erro nomeado ou travar até liberar, para exercitar os estados.
type baixadorFake struct {
	erro     error
	base     string
	transc   string        // conteúdo a gravar em transcricao.txt
	liberar  chan struct{} // se != nil, BaixarLegenda espera fechar antes de retornar
	chamadas int
	mu       sync.Mutex
}

func (b *baixadorFake) BaixarLegenda(ctx context.Context, ped *pipeline.Pedido) error {
	if b.liberar != nil {
		<-b.liberar
	}
	b.mu.Lock()
	b.chamadas++
	b.mu.Unlock()
	if b.erro != nil {
		ped.Status = pipeline.EstadoErro
		ped.Erro = b.erro.Error()
		return b.erro
	}
	dir := filepath.Join(b.base, ped.ID)
	os.MkdirAll(dir, 0755)
	return os.WriteFile(filepath.Join(dir, "transcricao.txt"), []byte(b.transc), 0644)
}

// selecionadorFake devolve candidatos fixos (ou erro) sem chamar o modelo.
type selecionadorFake struct {
	cands []validacao.Candidato
	erro  error
	visto string // último transcricaoPath recebido
}

func (s *selecionadorFake) Selecionar(ctx context.Context, transcricaoPath string) ([]validacao.Candidato, error) {
	s.visto = transcricaoPath
	if s.erro != nil {
		return nil, s.erro
	}
	return s.cands, nil
}

// servidorTeste monta um Servidor com id determinístico e relógio fixo.
func servidorTeste(t *testing.T, b BaixadorLegenda, sel Selecionador) *Servidor {
	t.Helper()
	base := t.TempDir()
	if bf, ok := b.(*baixadorFake); ok {
		bf.base = base // grava a transcrição na mesma raiz do servidor
	}
	n := 0
	return Novo(Opcoes{
		Baixador:       b,
		Selecionador:   sel,
		BaseDir:        base,
		LogRodadasPath: filepath.Join(base, "rodadas.md"), // isola o log no TempDir
		TemposPath:     filepath.Join(base, "tempos.csv"),
		AjustesPath:    filepath.Join(base, "ajustes.csv"), // idem para a auditoria de tempos
		Agora:          func() time.Time { return time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC) },
		GerarID:        func() string { n++; return "teste-1" },
	})
}

// esperarStatus faz polling do registro em memória até bater o estado ou estourar.
func esperarStatus(t *testing.T, s *Servidor, id string, quer pipeline.Estado) {
	t.Helper()
	prazo := time.Now().Add(2 * time.Second)
	for time.Now().Before(prazo) {
		s.mu.Lock()
		reg, ok := s.pedidos[id]
		var st pipeline.Estado
		if ok {
			st = reg.ped.Status
		}
		s.mu.Unlock()
		if ok && st == quer {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout esperando status %q do pedido %s", quer, id)
}

// --- Testes ---

func TestIndexServeAPagina(t *testing.T) {
	s := servidorTeste(t, &baixadorFake{}, &selecionadorFake{})
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("código = %d, quero 200", rec.Code)
	}
	corpo := rec.Body.String()
	if !strings.Contains(corpo, "<form") || !strings.Contains(corpo, `name="youtube_url"`) {
		t.Error("a página não traz o formulário de entrada")
	}
	if !strings.Contains(corpo, "htmx.org") {
		t.Error("a página não inclui o HTMX")
	}
}

func TestCriarPedidoValidoDisparaFaseLeve(t *testing.T) {
	b := &baixadorFake{transc: "[00:00:00] a graça de Deus basta."}
	sel := &selecionadorFake{cands: []validacao.Candidato{
		{Hook: "A graça basta", Start: "00:00:00", End: "00:00:30", DurationSeconds: 30, Score: 80},
	}}
	s := servidorTeste(t, b, sel)

	form := url.Values{"youtube_url": {"https://www.youtube.com/watch?v=abc"}, "inicio": {"00:00:00"}, "fim": {"00:10:00"}}
	req := httptest.NewRequest("POST", "/pedidos", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("código = %d, quero 200 (fragmento HTMX)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "teste-1") {
		t.Errorf("resposta não trouxe o id do pedido: %q", rec.Body.String())
	}

	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)

	// Selecionador recebeu o caminho da transcrição na pasta do pedido.
	if !strings.HasSuffix(sel.visto, filepath.Join("teste-1", "transcricao.txt")) {
		t.Errorf("selecionador viu caminho inesperado: %q", sel.visto)
	}
	// Candidatos persistidos em disco (fonte única para a fase pesada).
	if _, err := os.Stat(filepath.Join(s.baseDir, "teste-1", "candidatos.corrigido.json")); err != nil {
		t.Errorf("candidatos.corrigido.json não foi gravado: %v", err)
	}
}

func TestCriarPedidoJSONRetorna201ComID(t *testing.T) {
	s := servidorTeste(t, &baixadorFake{transc: "x"}, &selecionadorFake{})
	corpo := `{"youtube_url":"https://youtu.be/abc","inicio":"00:00:00","fim":"00:05:00"}`
	req := httptest.NewRequest("POST", "/pedidos", strings.NewReader(corpo))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("código = %d, quero 201", rec.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json inválido: %v", err)
	}
	if resp["id"] != "teste-1" {
		t.Errorf("id = %q, quero teste-1", resp["id"])
	}
	// Espera a fase leve terminar antes do cleanup do TempDir (goroutine grava em disco).
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)
}

func TestCriarEntradaInvalida(t *testing.T) {
	casos := []struct {
		nome             string
		url, inicio, fim string
	}{
		{"url vazia", "", "00:00:00", "00:10:00"},
		{"url não-youtube", "https://vimeo.com/123", "00:00:00", "00:10:00"},
		{"tempo mal formatado", "https://youtu.be/x", "abc", "00:10:00"},
		{"fim antes do início", "https://youtu.be/x", "00:10:00", "00:05:00"},
		{"fim igual ao início", "https://youtu.be/x", "00:05:00", "00:05:00"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			b := &baixadorFake{transc: "x"}
			s := servidorTeste(t, b, &selecionadorFake{})
			body := `{"youtube_url":"` + c.url + `","inicio":"` + c.inicio + `","fim":"` + c.fim + `"}`
			req := httptest.NewRequest("POST", "/pedidos", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json")
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("código = %d, quero 400", rec.Code)
			}
			// Nenhum pedido criado, nenhuma fase leve disparada.
			time.Sleep(20 * time.Millisecond)
			b.mu.Lock()
			ch := b.chamadas
			b.mu.Unlock()
			if ch != 0 {
				t.Errorf("entrada inválida disparou o download (%d chamadas)", ch)
			}
		})
	}
}

func TestStatusSemLegendaMostraErroSemBaixarVideo(t *testing.T) {
	// DP-001: sem legenda automática, a fase leve para com erro claro.
	b := &baixadorFake{erro: download.ErrSemLegenda}
	s := servidorTeste(t, b, &selecionadorFake{})

	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoErro)

	vis := statusJSONDoPedido(t, s, "teste-1")
	if vis.Status != string(pipeline.EstadoErro) {
		t.Fatalf("status = %q, quero erro", vis.Status)
	}
	if !strings.Contains(vis.Erro, "legenda automática") {
		t.Errorf("mensagem de erro pouco clara: %q", vis.Erro)
	}
}

func TestStatusErroNaSelecao(t *testing.T) {
	b := &baixadorFake{transc: "x"}
	sel := &selecionadorFake{erro: context.DeadlineExceeded}
	s := servidorTeste(t, b, sel)

	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoErro)

	vis := statusJSONDoPedido(t, s, "teste-1")
	if !strings.Contains(vis.Erro, "seleção") {
		t.Errorf("erro deveria citar a seleção: %q", vis.Erro)
	}
}

func TestStatusEmProgressoContinuaPolling(t *testing.T) {
	// Trava o download para pegar o estado intermediário e conferir que o fragmento
	// HTML mantém o polling (hx-trigger every 2s).
	liberar := make(chan struct{})
	b := &baixadorFake{transc: "x", liberar: liberar}
	s := servidorTeste(t, b, &selecionadorFake{})
	criarPedidoOK(t, s)

	esperarStatus(t, s, "teste-1", pipeline.EstadoBaixandoLegenda)
	req := httptest.NewRequest("GET", "/pedidos/teste-1", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	corpo := rec.Body.String()
	if !strings.Contains(corpo, `hx-trigger="every 2s"`) {
		t.Errorf("fragmento em progresso deveria manter o polling: %q", corpo)
	}
	close(liberar)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)
}

func TestStatusConcluidoListaCandidatosEmHTML(t *testing.T) {
	sel := &selecionadorFake{cands: []validacao.Candidato{
		{Hook: "O amor de Cristo", Start: "00:01:00", End: "00:01:40", DurationSeconds: 40, Score: 88},
		{Hook: "Jesus é Deus", Start: "00:02:00", End: "00:02:30", DurationSeconds: 30, Score: 75,
			RequerRevisaoReforcada: true, MotivoRevisao: "possível problema de fidelidade — revisar"},
	}}
	s := servidorTeste(t, &baixadorFake{transc: "x"}, sel)
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)

	req := httptest.NewRequest("GET", "/pedidos/teste-1", nil) // sem Accept json => HTML
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	corpo := rec.Body.String()

	if strings.Contains(corpo, `hx-trigger="every 2s"`) {
		t.Error("estado final não deveria continuar o polling")
	}
	// A tela de revisão embute os dados num <script> JSON e a estrutura da UI (trilha,
	// rodapé, botões). Os hooks e o motivo de revisão vão no payload JSON.
	for _, quer := range []string{
		`id="dados-trechos"`, `id="trilha"`, "Confirmar e gerar", // estrutura da tela
		"O amor de Cristo", "Jesus é Deus", // hooks no payload
		"possível problema de fidelidade", // motivo de revisão no payload
	} {
		if !strings.Contains(corpo, quer) {
			t.Errorf("HTML não trouxe %q:\n%s", quer, corpo)
		}
	}
	// O trecho marcado deve chegar com revisar=true no payload.
	if !strings.Contains(corpo, `"revisar":true`) {
		t.Errorf("trecho marcado deveria vir com revisar:true no JSON:\n%s", corpo)
	}
}

func TestStatusJSONContrato(t *testing.T) {
	sel := &selecionadorFake{cands: []validacao.Candidato{
		{Hook: "H", Start: "00:00:10", End: "00:00:40", DurationSeconds: 30, Score: 70, RequerRevisaoReforcada: true},
	}}
	s := servidorTeste(t, &baixadorFake{transc: "x"}, sel)
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)

	vis := statusJSONDoPedido(t, s, "teste-1")
	if vis.ID != "teste-1" || vis.Status != string(pipeline.EstadoAguardandoAprovacao) {
		t.Fatalf("visão inesperada: %+v", vis)
	}
	if len(vis.Candidatos) != 1 {
		t.Fatalf("quero 1 candidato, veio %d", len(vis.Candidatos))
	}
	c := vis.Candidatos[0]
	if c.Indice != 0 || c.Hook != "H" || c.Start != "00:00:10" || c.DurationSeconds != 30 ||
		c.Score != 70 || !c.RequerRevisaoReforcada {
		t.Errorf("candidato JSON fora do contrato: %+v", c)
	}
}

func TestStatusPedidoInexistente404(t *testing.T) {
	s := servidorTeste(t, &baixadorFake{}, &selecionadorFake{})
	req := httptest.NewRequest("GET", "/pedidos/nao-existe", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("código = %d, quero 404", rec.Code)
	}
}

// --- Helpers de teste ---

func criarPedidoOK(t *testing.T, s *Servidor) {
	t.Helper()
	body := `{"youtube_url":"https://youtu.be/abc","inicio":"00:00:00","fim":"00:10:00"}`
	req := httptest.NewRequest("POST", "/pedidos", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("criação falhou: código %d, corpo %q", rec.Code, rec.Body.String())
	}
}

func statusJSONDoPedido(t *testing.T, s *Servidor, id string) statusJSON {
	t.Helper()
	req := httptest.NewRequest("GET", "/pedidos/"+id, nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status código = %d", rec.Code)
	}
	var vis statusJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &vis); err != nil {
		t.Fatalf("json inválido: %v (%s)", err, rec.Body.String())
	}
	return vis
}
