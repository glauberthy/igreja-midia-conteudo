package servidor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"srtclean/internal/pipeline"
	"srtclean/internal/validacao"
)

// prepararPedidoEmDisco monta um pedido completo em disco, como o servidor deixa.
func prepararPedidoEmDisco(t *testing.T, base, id string, comPedidoJSON, comVideo bool) {
	t.Helper()
	dir := filepath.Join(base, id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "transcricao.txt"), []byte(transcricaoAjuste()), 0644)

	doc := struct {
		Candidatos []validacao.Candidato `json:"candidatos"`
	}{Candidatos: []validacao.Candidato{
		{Start: "00:00:36.000", End: "00:01:18.000", DurationSeconds: 42, Score: 90, Hook: "frase numero 6 termina aqui."},
		{Start: "00:01:00.000", End: "00:01:42.000", DurationSeconds: 42, Score: 85, Hook: "frase numero 10 termina aqui."},
	}}
	b, _ := json.MarshalIndent(doc, "", "  ")
	os.WriteFile(filepath.Join(dir, "candidatos.corrigido.json"), b, 0644)

	if comPedidoJSON {
		ped := &pipeline.Pedido{ID: id, YouTubeURL: "https://www.youtube.com/watch?v=abc12345678",
			Inicio: "00:00:00", Fim: "00:05:00"}
		if err := ped.Salvar(base); err != nil {
			t.Fatal(err)
		}
	} else {
		info := map[string]any{"webpage_url": "https://www.youtube.com/watch?v=abc12345678", "title": "Culto"}
		ib, _ := json.Marshal(info)
		os.WriteFile(filepath.Join(dir, "legenda.info.json"), ib, 0644)
	}
	if comVideo {
		os.WriteFile(filepath.Join(dir, "video.mp4"), make([]byte, 25<<20), 0644)
	}
}

// TestRetomarPoeOPedidoNaRevisao é o ganho central: sem refazer legenda nem seleção, o pedido
// reaparece pronto para revisar. Era o que custava ~40 s por iteração de teste.
func TestRetomarPoeOPedidoNaRevisao(t *testing.T) {
	s := servidorPesada(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})
	prepararPedidoEmDisco(t, s.baseDir, "antigo-1", true, true)

	if err := s.Retomar("antigo-1"); err != nil {
		t.Fatalf("retomar falhou: %v", err)
	}

	s.mu.Lock()
	reg, ok := s.pedidos["antigo-1"]
	s.mu.Unlock()
	if !ok {
		t.Fatal("o pedido não entrou no mapa")
	}
	if reg.ped.Status != pipeline.EstadoAguardandoAprovacao {
		t.Errorf("status = %q, queria aguardando-aprovacao", reg.ped.Status)
	}
	if len(reg.cands) != 2 {
		t.Errorf("carregou %d candidatos, esperava 2", len(reg.cands))
	}
	// O texto falado é reconstruído, não vem de cache: a revisão tem de mostrar o mesmo que
	// mostraria num pedido novo.
	if len(reg.textos) != 2 || reg.textos[0] == "" {
		t.Errorf("textos falados não foram reconstruídos: %#v", reg.textos)
	}
	// Métricas do ciclo ATUAL, não somadas às da execução original.
	if reg.metricas == nil || reg.metricas.BaixarLegendaMs != 0 || reg.metricas.SelecionarMs != 0 {
		t.Error("as métricas deveriam começar zeradas no pedido retomado")
	}
}

// TestRetomarReconstroiPedidoAntigo: pedidos anteriores à gravação de metadados não têm
// pedido.json. Sem a reconstrução, todo material já baixado ficaria fora do alcance da
// retomada — inclusive o vídeo de 900 MB que serve de material de teste.
func TestRetomarReconstroiPedidoAntigo(t *testing.T) {
	s := servidorPesada(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})
	prepararPedidoEmDisco(t, s.baseDir, "antigo-2", false, true) // sem pedido.json

	if err := s.Retomar("antigo-2"); err != nil {
		t.Fatalf("retomar deveria reconstruir do legenda.info.json: %v", err)
	}
	s.mu.Lock()
	ped := s.pedidos["antigo-2"].ped
	s.mu.Unlock()
	if ped.YouTubeURL == "" {
		t.Error("a URL não foi recuperada do legenda.info.json")
	}
	// Inicio 00:00:00: o vídeo em disco é o INTEIRO, e o cmd/render usa ped.Inicio como origem
	// do arquivo. Vazio faria o cmd/render recusar com "início inválido".
	if ped.Inicio != "00:00:00" {
		t.Errorf("Inicio = %q; o vídeo completo tem origem zero", ped.Inicio)
	}
	// E grava o pedido.json, para a próxima retomada não precisar reconstruir de novo.
	if _, err := os.Stat(filepath.Join(s.baseDir, "antigo-2", "pedido.json")); err != nil {
		t.Error("o pedido reconstruído deveria ser gravado")
	}
}

// TestRetomarRecusaComMensagemUtil: falhar na subida com o motivo é melhor que subir e o
// operador encontrar uma tela vazia sem entender por quê.
func TestRetomarRecusaComMensagemUtil(t *testing.T) {
	s := servidorPesada(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})

	if err := s.Retomar("nao-existe"); err == nil || !strings.Contains(err.Error(), "não existe") {
		t.Errorf("pedido inexistente deveria falhar com motivo claro: %v", err)
	}

	// Pasta existe mas sem candidatos: nada a revisar.
	dir := filepath.Join(s.baseDir, "vazio-1")
	os.MkdirAll(dir, 0755)
	ped := &pipeline.Pedido{ID: "vazio-1", YouTubeURL: "https://x", Inicio: "00:00:00"}
	ped.Salvar(s.baseDir)
	err := s.Retomar("vazio-1")
	if err == nil || !strings.Contains(err.Error(), "candidatos") {
		t.Errorf("pedido sem candidatos deveria falhar nomeando o que falta: %v", err)
	}
}

// TestRetomarNaoQuebraAAutocura é a fronteira que a spec-06 depende: retomar é EXPLÍCITO. Um
// servidor novo continua nascendo com o mapa vazio, senão pedido travado por crash viraria
// vazamento permanente de disco.
func TestRetomarNaoQuebraAAutocura(t *testing.T) {
	s := servidorPesada(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})
	prepararPedidoEmDisco(t, s.baseDir, "antigo-3", true, true)

	// Servidor novo sobre a MESMA pasta, sem retomar: não carrega nada.
	s2 := Novo(Opcoes{
		Baixador: &baixadorFake{transc: "x", base: s.baseDir}, Selecionador: candsJanela(),
		BaseDir: s.baseDir, OutDir: t.TempDir(),
		LogRodadasPath: filepath.Join(s.baseDir, "r.md"),
		TemposPath:     filepath.Join(s.baseDir, "t.csv"),
		CortesPath:     filepath.Join(s.baseDir, "c.csv"),
	})
	if n := len(s2.pedidos); n != 0 {
		t.Fatalf("servidor novo carregou %d pedido(s) sem -retomar — a autocura depende de não carregar", n)
	}
}

// TestVideoUsavelRejeitaResiduo: um .part renomeado ou download morto na metade passaria por
// "existe" e faria o render falhar com erro de ffmpeg, longe da causa.
func TestVideoUsavelRejeitaResiduo(t *testing.T) {
	dir := t.TempDir()
	casos := []struct {
		nome  string
		bytes int
		quer  bool
	}{
		{"vazio", 0, false},
		{"parcial pequeno", 1 << 20, false},
		{"plausível", 25 << 20, true},
	}
	for _, c := range casos {
		p := filepath.Join(dir, strings.ReplaceAll(c.nome, " ", "_")+".mp4")
		os.WriteFile(p, make([]byte, c.bytes), 0644)
		if got := videoUsavel(p); got != c.quer {
			t.Errorf("%s (%d bytes): videoUsavel = %v, queria %v", c.nome, c.bytes, got, c.quer)
		}
	}
	if videoUsavel(filepath.Join(dir, "nao-existe.mp4")) {
		t.Error("arquivo inexistente não pode ser usável")
	}
}

// TestCriarGravaPedidoJSON prova, pelo caminho HTTP real, que criar um pedido deixa o
// pedido.json em disco — sem o qual cmd/render, cmd/auditar e cmd/limpar não conseguem
// trabalhar sobre pedidos do servidor. Imprime o conteúdo para inspeção com -v.
func TestCriarGravaPedidoJSON(t *testing.T) {
	s := servidorPesada(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", "aguardando-aprovacao")

	p := filepath.Join(s.baseDir, "teste-1", "pedido.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("o servidor NÃO gravou o pedido.json: %v", err)
	}
	t.Logf("pedido.json gravado em %s:\n%s", p, b)

	var ped pipeline.Pedido
	if err := json.Unmarshal(b, &ped); err != nil {
		t.Fatalf("pedido.json ilegível: %v", err)
	}
	if ped.ID != "teste-1" || ped.YouTubeURL == "" {
		t.Errorf("pedido.json sem os metadados essenciais: %+v", ped)
	}
}

// TestNovoNaoLeDiscoNoBoot é a outra metade: gravar não pode ter virado carregar. A autocura da
// spec-06 depende de o mapa nascer vazio — é isso que faz um pedido travado por crash desaparecer
// no restart e o material bruto voltar a ser limpável.
//
// Verifica o comportamento (mapa vazio com pedidos completos em disco), não a ausência de uma
// chamada no código.
func TestNovoNaoLeDiscoNoBoot(t *testing.T) {
	base := t.TempDir()
	// Três pedidos completos em disco, com pedido.json — o cenário que tentaria o boot.
	for _, id := range []string{"p1", "p2", "p3"} {
		prepararPedidoEmDisco(t, base, id, true, true)
	}

	s := Novo(Opcoes{
		Baixador: &baixadorFake{transc: "x", base: base}, Selecionador: candsJanela(),
		BaseDir: base, OutDir: t.TempDir(),
		LogRodadasPath: filepath.Join(base, "r.md"),
		TemposPath:     filepath.Join(base, "t.csv"),
		CortesPath:     filepath.Join(base, "c.csv"),
	})
	s.mu.Lock()
	n := len(s.pedidos)
	s.mu.Unlock()
	t.Logf("pedidos em disco: 3 (com pedido.json) | carregados no boot: %d", n)
	if n != 0 {
		t.Fatalf("o boot carregou %d pedido(s): a autocura da spec-06 quebrou — pedido travado "+
			"por crash viraria vazamento permanente de disco", n)
	}
}
