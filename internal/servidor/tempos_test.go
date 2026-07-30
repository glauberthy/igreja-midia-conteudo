package servidor

import (
	"encoding/csv"
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

	// Mesmo número de CAMPOS do cabeçalho. Desde que nome e valor saem da mesma lista
	// (colunasTempos), divergir de contagem virou impossível por construção — o que este teste
	// ainda pega é o risco que sobrou: um valor que escapa vírgula errado e parte a linha em
	// dois campos. Por isso a fixture tem título COM vírgula.
	//
	// Contado com encoding/csv, não com strings.Count: contar vírgula em linha com campo entre
	// aspas mede a coisa errada — foi exatamente o erro que a primeira versão da migração do
	// cabeçalho cometeu.
	nCab := len(strings.Split(strings.TrimRight(cabecalhoTempos, "\n"), ","))
	reg, err := csv.NewReader(strings.NewReader(linha)).ReadAll()
	if err != nil || len(reg) != 1 {
		t.Fatalf("a linha não é um CSV de um registro: %v (%q)", err, linha)
	}
	if len(reg[0]) != nCab {
		t.Errorf("a linha tem %d campos e o cabeçalho %d: CSV torto quebra a análise",
			len(reg[0]), nCab)
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
	s := Novo(Opcoes{Baixador: &baixadorFake{}, Selecionador: &selecionadorFake{}, TemposPath: csv,
		CortesPath: filepath.Join(t.TempDir(), "cortes.csv"),
		AcoesPath:  filepath.Join(t.TempDir(), "acoes.csv")})

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
	// acusa, com razão, leitura concorrente no próprio teste). O hook é do SERVIDOR (não
	// global): assim não há corrida entre restaurar a variável e a goroutine que ainda loga.
	var mu sync.Mutex
	var resumos []string
	bv := &baixadorVideoFake{}
	rf := &renderFake{}
	s := servidorPesada(t, candsJanela(), bv, rf)
	s.logTemposFn = func(m string) { mu.Lock(); resumos = append(resumos, m); mu.Unlock() }
	criarPedidoOK(t, s) // janela do pedido: 00:00:00 → 00:10:00
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, "teste-1", []int{0, 1})
	esperarStatus(t, s, "teste-1", pipeline.EstadoConcluido)
	esperarArquivo(t, s.temposPath) // idem: persistência vem depois do status

	// Resumo no log do servidor.
	// FILTRA o resumo: o logTempos também recebe avisos de cache, de download e das pausas.
	// A versão anterior contava toda mensagem e passou a falhar quando a fase leve ganhou o
	// preparo do vídeo — o teste media o canal, não o resumo.
	mu.Lock()
	defer mu.Unlock()
	var resumo []string
	for _, r := range resumos {
		if strings.Contains(r, "TOTAL DE MÁQUINA") || strings.Contains(r, "FALHOU") {
			resumo = append(resumo, r)
		}
	}
	if len(resumo) != 1 {
		t.Fatalf("esperava 1 resumo de tempos no log, veio %d (de %d mensagens)", len(resumo), len(resumos))
	}
	resumos = resumo
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
	bv := &baixadorVideoFake{erro: fmt.Errorf("googlevideo timeout")}
	rf := &renderFake{}
	s := servidorPesada(t, candsJanela(), bv, rf)
	s.logTemposFn = func(m string) { mu.Lock(); resumos = append(resumos, m); mu.Unlock() }
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, "teste-1", []int{0})
	esperarStatus(t, s, "teste-1", pipeline.EstadoErro)
	esperarArquivo(t, s.temposPath) // o CSV é gravado logo após o status virar erro

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
	achouFalhou := false
	for _, r := range resumos {
		if strings.Contains(r, "FALHOU") {
			achouFalhou = true
		}
	}
	if !achouFalhou {
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
