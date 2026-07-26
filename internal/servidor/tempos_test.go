package servidor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"srtclean/internal/pipeline"
)

func TestMetricasTotalExcluiEsperaHumana(t *testing.T) {
	m := &Metricas{
		BaixarLegendaMs: 3000, SelecionarMs: 40000, ValidarMs: 500,
		BaixarVideoMs: 8000, RenderizarMs: 8000,
		AguardandoMs: 3600000, // 1h de revisão humana
		NumAprovados: 2,
	}
	// 3+40+0,5+8+8 = 59,5s de máquina. A espera humana NÃO entra: senão um pedido revisado
	// no dia seguinte pareceria uma falha de desempenho.
	if got := m.TotalMaquinaMs(); got != 59500 {
		t.Errorf("total de máquina = %d ms, quero 59500 (sem a espera humana)", got)
	}
	if got := m.RenderPorShortMs(); got != 4000 {
		t.Errorf("render por short = %d ms, quero 4000 (8s / 2 shorts)", got)
	}
}

func TestMetricasLinhaCSVeCabecalho(t *testing.T) {
	m := &Metricas{
		ID: "web-1", Titulo: "Pr. Fulano, 19/07 | Culto", Quando: time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC),
		DuracaoSermaoS: 3600, TokensTranscricao: 9300, NumCandidatos: 5, NumAprovados: 2,
		BytesVideo: 125 * 1024 * 1024, Retries: 1,
		BaixarLegendaMs: 3000, SelecionarMs: 40000, ValidarMs: 500,
		BaixarVideoMs: 8000, RenderizarMs: 8000, AguardandoMs: 120000,
	}
	linha := m.LinhaCSV()

	// Mesmo número de colunas do cabeçalho (senão o CSV fica torto e a análise quebra).
	nCab := strings.Count(cabecalhoTempos, ",")
	nLin := strings.Count(linha, ",")
	// O título tem vírgula: vem entre aspas, então conta uma vírgula extra dentro do campo.
	if nLin != nCab+1 {
		t.Errorf("colunas: cabeçalho tem %d vírgulas, linha tem %d (título entre aspas soma 1)", nCab, nLin)
	}
	// Título com vírgula e | precisa vir protegido por aspas.
	if !strings.Contains(linha, `"Pr. Fulano, 19/07 | Culto"`) {
		t.Errorf("título com vírgula deveria vir entre aspas: %s", linha)
	}
	for _, q := range []string{"web-1", "3600", "9300", "125.0", "59.5", "120.0"} {
		if !strings.Contains(linha, q) {
			t.Errorf("faltou %q na linha: %s", q, linha)
		}
	}
}

func TestGravarTemposAnexaComCabecalhoUmaVez(t *testing.T) {
	csv := filepath.Join(t.TempDir(), "tempos.csv")
	s := Novo(Opcoes{Baixador: &baixadorFake{}, Selecionador: &selecionadorFake{}, TemposPath: csv})

	s.gravarTempos(&Metricas{ID: "p1", NumAprovados: 1, RenderizarMs: 1000})
	s.gravarTempos(&Metricas{ID: "p2", NumAprovados: 1, RenderizarMs: 2000})

	b, err := os.ReadFile(csv)
	if err != nil {
		t.Fatalf("CSV não foi escrito: %v", err)
	}
	linhas := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(linhas) != 3 { // cabeçalho + 2 pedidos
		t.Fatalf("esperava 3 linhas (cabeçalho + 2), veio %d:\n%s", len(linhas), b)
	}
	if !strings.HasPrefix(linhas[0], "quando,pedido,titulo") {
		t.Errorf("primeira linha deveria ser o cabeçalho: %q", linhas[0])
	}
	if strings.Contains(linhas[1], "quando,pedido") || strings.Contains(linhas[2], "quando,pedido") {
		t.Error("cabeçalho repetido nas linhas de dados")
	}
}

// Integração: o ciclo completo preenche as métricas, loga o resumo e grava a linha.
func TestCicloRegistraTempos(t *testing.T) {
	// O resumo é emitido pela goroutine da fase pesada: protege o slice (senão o -race
	// acusa, com razão, leitura concorrente no próprio teste).
	var mu sync.Mutex
	var resumos []string
	orig := LogTempos
	LogTempos = func(m string) { mu.Lock(); resumos = append(resumos, m); mu.Unlock() }
	t.Cleanup(func() { LogTempos = orig })

	bv := &baixadorVideoFake{}
	rf := &renderFake{}
	s := servidorPesada(t, candsJanela(), bv, rf)
	criarPedidoOK(t, s) // janela do pedido: 00:00:00 → 00:10:00
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, "teste-1", []int{0, 1})
	esperarStatus(t, s, "teste-1", pipeline.EstadoConcluido)

	// Resumo no log do servidor.
	mu.Lock()
	defer mu.Unlock()
	if len(resumos) != 1 {
		t.Fatalf("esperava 1 resumo de tempos no log, veio %d", len(resumos))
	}
	for _, q := range []string{"TOTAL DE MÁQUINA", "legenda", "selecionar", "renderizar", "espera humana"} {
		if !strings.Contains(resumos[0], q) {
			t.Errorf("resumo não menciona %q: %s", q, resumos[0])
		}
	}

	// Linha no CSV, com o contexto que explica variação.
	b, err := os.ReadFile(s.temposPath)
	if err != nil {
		t.Fatalf("CSV de tempos não foi escrito: %v", err)
	}
	linhas := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(linhas) != 2 {
		t.Fatalf("esperava cabeçalho + 1 pedido, veio %d linhas:\n%s", len(linhas), b)
	}
	campos := strings.Split(linhas[1], ",")
	if campos[1] != "teste-1" {
		t.Errorf("pedido na coluna errada: %v", campos)
	}
	if campos[3] != "600" { // 00:00:00 → 00:10:00 = 600s
		t.Errorf("duração do sermão = %q, quero 600", campos[3])
	}
	if campos[5] != "3" || campos[6] != "2" { // 3 candidatos, 2 aprovados
		t.Errorf("candidatos/aprovados = %q/%q, quero 3/2", campos[5], campos[6])
	}
}

// Pedidos que FALHAM também entram no CSV. Antes, gravarTempos ficava depois dos setErro
// (que fazem return), então só o sucesso era registrado — e a média ficava otimista,
// escondendo justamente o tempo perdido que o operador sente e vai refazer.
func TestPedidoQueFalhaEntraNoCSV(t *testing.T) {
	var mu sync.Mutex
	var resumos []string
	orig := LogTempos
	LogTempos = func(m string) { mu.Lock(); resumos = append(resumos, m); mu.Unlock() }
	t.Cleanup(func() { LogTempos = orig })

	bv := &baixadorVideoFake{erro: fmt.Errorf("googlevideo timeout")}
	rf := &renderFake{}
	s := servidorPesada(t, candsJanela(), bv, rf)
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, "teste-1", []int{0})
	esperarStatus(t, s, "teste-1", pipeline.EstadoErro)

	b, err := os.ReadFile(s.temposPath)
	if err != nil {
		t.Fatalf("pedido que falhou NÃO foi registrado no CSV: %v", err)
	}
	linhas := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(linhas) != 2 {
		t.Fatalf("esperava cabeçalho + 1 linha, veio %d:\n%s", len(linhas), b)
	}
	// Marcado como NÃO completado, com o motivo.
	if !strings.Contains(linhas[1], ",nao,") {
		t.Errorf("a linha deveria marcar completou=nao: %s", linhas[1])
	}
	if !strings.Contains(linhas[1], "timeout") {
		t.Errorf("a linha deveria trazer o motivo da falha: %s", linhas[1])
	}
	// E o tempo até a falha foi contabilizado (não é zero).
	mu.Lock()
	defer mu.Unlock()
	if len(resumos) != 1 || !strings.Contains(resumos[0], "FALHOU") {
		t.Errorf("resumo no log deveria dizer que falhou: %v", resumos)
	}
}

// Só UMA linha por pedido, mesmo que setErro seja chamado mais de uma vez.
func TestFinalizarGravaUmaVezSo(t *testing.T) {
	bv := &baixadorVideoFake{}
	rf := &renderFake{}
	s := servidorPesada(t, candsJanela(), bv, rf)
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)

	s.mu.Lock()
	reg := s.pedidos["teste-1"]
	s.mu.Unlock()
	s.finalizarPedido(reg, "primeiro erro")
	s.finalizarPedido(reg, "segundo erro") // não deve gravar de novo

	b, _ := os.ReadFile(s.temposPath)
	linhas := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(linhas) != 2 {
		t.Errorf("esperava 1 linha de dados, veio %d:\n%s", len(linhas)-1, b)
	}
}

func TestContarRetryAtribuiAoPedido(t *testing.T) {
	m := &Metricas{}
	m.IniciarPedido(time.Now())
	ContarRetry()
	ContarRetry()
	m.FecharRetries()
	if m.Retries != 2 {
		t.Errorf("retries = %d, quero 2", m.Retries)
	}
	// Um pedido seguinte não herda os retries do anterior.
	m2 := &Metricas{}
	m2.IniciarPedido(time.Now())
	m2.FecharRetries()
	if m2.Retries != 0 {
		t.Errorf("pedido novo herdou retries: %d", m2.Retries)
	}
}

func TestDuracaoJanelaS(t *testing.T) {
	casos := []struct {
		ini, fim string
		quer     int
	}{
		{"00:00:00", "00:10:00", 600},
		{"01:29:39", "02:05:12", 2133},
		{"00:10:00", "00:05:00", 0}, // invertido
		{"xx", "00:05:00", 0},       // inválido
	}
	for _, c := range casos {
		if got := duracaoJanelaS(c.ini, c.fim); got != c.quer {
			t.Errorf("duracaoJanelaS(%q,%q) = %d, quero %d", c.ini, c.fim, got, c.quer)
		}
	}
}
