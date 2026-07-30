package servidor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"srtclean/internal/pipeline"
)

// O ABANDONO era o único desfecho invisível no tempos.csv: quem termina escreve (concluído ou
// erro), quem é interrompido não escrevia nada. Mesmo viés do cortes.csv registrando só os
// trechos ajustados — a medição fica com os ciclos que chegaram ao fim, e a média mente para o
// lado otimista. Aconteceu de verdade: um pedido de medição desapareceu do CSV quando o
// servidor caiu para uma correção.

// TestPedidoEmCursoNoEncerramentoEntraNoCSV é o teste do registro.
func TestPedidoEmCursoNoEncerramentoEntraNoCSV(t *testing.T) {
	// O canal NUNCA é fechado: a goroutine da fase leve fica parada nele, que é justamente o
	// estado "em curso" que se quer registrar. Soltá-la no fim do teste (o instinto de limpar)
	// fazia a fase continuar escrevendo na pasta temporária DEPOIS do teste, e o cleanup do
	// t.TempDir falhava com "directory not empty". Goroutine parada morre com o processo de
	// teste e não escreve nada.
	liberar := make(chan struct{})
	b := &baixadorFake{transc: transcricaoLongaDoCulto(), liberar: liberar}
	s := servidorTeste(t, b, candsJanela())

	id := criarPedido(t, s, "https://youtu.be/cultoTeste1", "00:00:00", "00:10:00")
	esperarStatus(t, s, id, pipeline.EstadoBaixandoLegenda) // travado na fase leve, em curso

	n := s.RegistrarAbandonados("")
	if n != 1 {
		t.Fatalf("registrou %d pedido(s) abandonado(s), quero 1", n)
	}
	esperarArquivo(t, s.temposPath)

	reg, err := lerCSV(t, s.temposPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg) != 2 {
		t.Fatalf("esperava cabeçalho + 1 linha, veio %d", len(reg))
	}
	campo := func(nome string) string {
		for i, c := range reg[0] {
			if c == nome {
				return reg[1][i]
			}
		}
		t.Fatalf("coluna %q não existe", nome)
		return ""
	}
	if campo("pedido") != id {
		t.Errorf("a linha é de outro pedido: %q", campo("pedido"))
	}
	if campo("completou") != "nao" {
		t.Errorf("completou = %q; um pedido interrompido NÃO completou", campo("completou"))
	}
	if !strings.Contains(campo("erro"), "encerrado") {
		t.Errorf("o motivo não diz que o servidor encerrou: %q", campo("erro"))
	}
	// O tempo gasto até o abandono é real e entra: é o que responde "o operador esperou quanto
	// antes de desistir?".
	if campo("quando") == "" || strings.HasPrefix(campo("quando"), "0001") {
		t.Errorf("linha de abandono sem data (%q): não daria para ordenar nem filtrar", campo("quando"))
	}
}

// TestAbandonoNaoDuplicaPedidoJaTerminado: chamar duas vezes, ou chamar depois de um pedido ter
// concluído, não pode inventar linha. O finalizarPedido zera as métricas ao registrar, e é essa
// marca que garante idempotência — sem ela, um Ctrl-C depois de tudo pronto poluiria o CSV.
func TestAbandonoNaoDuplicaPedidoJaTerminado(t *testing.T) {
	bv := &baixadorVideoFake{}
	rf := &renderFake{}
	s := servidorComCache(t, candsJanela(), bv, rf)

	id := criarPedido(t, s, "https://youtu.be/cultoTeste1", "00:00:00", "00:10:00")
	esperarStatus(t, s, id, pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, id, []int{0})
	esperarStatus(t, s, id, pipeline.EstadoConcluido)
	esperarArquivo(t, s.temposPath)

	antes := linhasDoCSV(t, s.temposPath)
	if n := s.RegistrarAbandonados(""); n != 0 {
		t.Errorf("registrou %d abandono(s) num pedido já concluído", n)
	}
	if n := s.RegistrarAbandonados(""); n != 0 {
		t.Errorf("segunda chamada registrou %d abandono(s)", n)
	}
	if depois := linhasDoCSV(t, s.temposPath); depois != antes {
		t.Errorf("o CSV passou de %d para %d linhas: abandono duplicou registro", antes, depois)
	}
}

// TestAbandonoAguardandoAprovacaoContaComoNaoConcluido: o pedido esperando o operador está EM
// CURSO. Se o servidor cai ali, o ciclo não terminou — e é justamente o caso mais comum de
// abandono real (o operador abriu, viu os trechos e não decidiu).
func TestAbandonoAguardandoAprovacaoContaComoNaoConcluido(t *testing.T) {
	bv := &baixadorVideoFake{}
	s := servidorComCache(t, candsJanela(), bv, &renderFake{})
	id := criarPedido(t, s, "https://youtu.be/cultoTeste1", "00:00:00", "00:10:00")
	esperarStatus(t, s, id, pipeline.EstadoAguardandoAprovacao)

	if n := s.RegistrarAbandonados(""); n != 1 {
		t.Fatalf("registrou %d; um pedido aguardando aprovação está em curso", n)
	}
	esperarArquivo(t, s.temposPath)
	if linhasDoCSV(t, s.temposPath) != 1 {
		t.Error("esperava exatamente uma linha de dados no CSV")
	}
}

// TestFaseEmCursoSobreviveAoAbandono é o teste do panic que o abandono destapou, e é o que
// prova que a causa foi REMOVIDA e não apenas cercada.
//
// Cenário: o servidor finaliza um pedido cuja fase leve ainda está rodando. Quando a fase
// termina, ela escreve as métricas dela. Enquanto o sinal de "já finalizado" era `metricas =
// nil`, essa escrita derrubava o processo — e derrubar o servidor no encerramento é perder
// justamente o registro que o abandono existe para gravar.
//
// Agora a fase escreve num struct que ninguém mais lê: nada quebra, e o CSV continua com uma
// linha só.
func TestFaseEmCursoSobreviveAoAbandono(t *testing.T) {
	liberar := make(chan struct{})
	b := &baixadorFake{transc: transcricaoLongaDoCulto(), liberar: liberar}
	s := servidorTeste(t, b, candsJanela())

	id := criarPedido(t, s, "https://youtu.be/cultoTeste1", "00:00:00", "00:10:00")
	esperarStatus(t, s, id, pipeline.EstadoBaixandoLegenda)
	if n := s.RegistrarAbandonados(""); n != 1 {
		t.Fatalf("registrou %d abandono(s), quero 1", n)
	}
	esperarArquivo(t, s.temposPath)

	// Solta a fase leve: ela vai até o fim e escreve ValidarMs num pedido JÁ finalizado.
	close(liberar)
	// Chegar a aguardando-aprovação é a última escrita da fase leve. Se o processo sobreviveu
	// até aqui, a escrita pós-finalização é inócua. (Também garante que a fase parou de escrever
	// antes de o t.TempDir ser removido.)
	esperarStatus(t, s, id, pipeline.EstadoAguardandoAprovacao)

	if n := linhasDoCSV(t, s.temposPath); n != 1 {
		t.Errorf("CSV com %d linha(s) de dados: a fase que continuou gravou de novo", n)
	}
}

func linhasDoCSV(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n := 0
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if l != "" {
			n++
		}
	}
	if n > 0 {
		n-- // desconta o cabeçalho
	}
	return n
}

// TestOEncerramentoDoComandoRegistraAbandono liga o handler de sinal ao registro: sem isto o
// método existiria e ninguém o chamaria — o caso "constante existe mas o caminho real usa
// outro" que este projeto já pagou três vezes.
//
// Verifica no FONTE do cmd/servidor, porque não há como mandar SIGTERM em si mesmo num teste
// sem derrubar o processo do `go test`. É verificação de ligação, não de comportamento — e está
// dito assim de propósito.
func TestOEncerramentoDoComandoRegistraAbandono(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "cmd", "servidor", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	fonte := string(b)
	for _, quer := range []string{"signal.Notify", "syscall.SIGTERM", "RegistrarAbandonados"} {
		if !strings.Contains(fonte, quer) {
			t.Errorf("cmd/servidor não tem %q: pedido em curso sumiria do CSV no encerramento", quer)
		}
	}
}
