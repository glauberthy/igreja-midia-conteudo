package servidor

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"srtclean/internal/pipeline"
)

// pedidoEmDisco cria a pasta de um pedido com material bruto e recência ANTIGA — o pior
// caso para a limpeza: pelos critérios de idade, ele é o primeiro a ser apagado.
func pedidoEmDisco(t *testing.T, base, id string) string {
	t.Helper()
	dir := filepath.Join(base, id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Error(err) // Error, não Fatal: este helper também roda em goroutine
		return dir
	}
	os.WriteFile(filepath.Join(dir, "video.mp4"), make([]byte, 4096), 0644)
	os.WriteFile(filepath.Join(dir, "candidatos.corrigido.json"), []byte("{}"), 0644)
	velho := time.Now().Add(-72 * time.Hour)
	os.Chtimes(filepath.Join(dir, "candidatos.corrigido.json"), velho, velho)
	os.Chtimes(dir, velho, velho)
	return dir
}

// registrarEmCurso põe no servidor um pedido em estado NÃO terminal, como se uma fase
// estivesse rodando nele agora.
func registrarEmCurso(s *Servidor, id string, estado pipeline.Estado) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pedidos[id] = &registro{ped: &pipeline.Pedido{ID: id, Status: estado}}
}

// TestLimpezaNaoApagaPedidoEmCurso é a invariante central da spec-06: um pedido que ainda
// está rodando é INVISÍVEL para a limpeza, mesmo sendo o mais antigo do disco. Não basta
// proteger o pedido que acabou de concluir — o perigo é apagar o video.mp4 de 900 MB que
// outro pedido acabou de baixar, em silêncio.
func TestLimpezaNaoApagaPedidoEmCurso(t *testing.T) {
	s := servidorPesada(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})
	s.reterPedidos = 1

	emCurso := pedidoEmDisco(t, s.baseDir, "pedido-em-curso")
	registrarEmCurso(s, "pedido-em-curso", pipeline.EstadoBaixandoVideo)

	recente := pedidoEmDisco(t, s.baseDir, "pedido-recente")
	agora := time.Now()
	os.Chtimes(filepath.Join(recente, "candidatos.corrigido.json"), agora, agora)

	descartavel := pedidoEmDisco(t, s.baseDir, "pedido-terminado")
	s.limparSobLock()

	if _, err := os.Stat(filepath.Join(emCurso, "video.mp4")); err != nil {
		t.Fatal("a limpeza apagou o video.mp4 de um pedido EM CURSO")
	}
	// Contraprova: sem a proteção, "pedido-terminado" (mesma idade) É apagado. Se ele
	// sobrevivesse, o teste acima passaria por acidente (limpeza não fez nada).
	if _, err := os.Stat(filepath.Join(descartavel, "video.mp4")); err == nil {
		t.Error("a limpeza não removeu nada — o teste da invariante não provaria nada")
	}
}

// TestLimpezaEstadoTerminalEhLimpavel fecha o outro lado: concluído e erro NÃO são
// protegidos, senão o disco nunca esvazia.
func TestLimpezaEstadoTerminalEhLimpavel(t *testing.T) {
	s := servidorPesada(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})
	s.reterPedidos = 1
	// Ocupa a única vaga de retenção com um pedido mais novo, para que os terminais
	// abaixo estejam de fato na faixa limpável.
	recente := pedidoEmDisco(t, s.baseDir, "pedido-recente")
	agora := time.Now()
	os.Chtimes(filepath.Join(recente, "candidatos.corrigido.json"), agora, agora)

	for _, c := range []struct {
		id     string
		estado pipeline.Estado
	}{
		{"terminado-ok", pipeline.EstadoConcluido},
		{"terminado-erro", pipeline.EstadoErro},
	} {
		dir := pedidoEmDisco(t, s.baseDir, c.id)
		registrarEmCurso(s, c.id, c.estado)
		s.limparSobLock()
		if _, err := os.Stat(filepath.Join(dir, "video.mp4")); err == nil {
			t.Errorf("%s: pedido em estado terminal deveria ser limpável", c.estado)
		}
	}
}

// TestLimpezaConcorrenteComPedidosNovos estressa a invariante: pedidos nascem e avançam
// enquanto a limpeza roda. Nenhum vídeo de pedido em curso pode sumir. Rodar com -race.
//
// -race sozinho não bastaria aqui: ele acusa acesso concorrente à memória, não "arquivo
// apagado indevidamente". A checagem de sobrevivência do arquivo é o que prova o ponto.
func TestLimpezaConcorrenteComPedidosNovos(t *testing.T) {
	s := servidorPesada(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})
	s.reterPedidos = 1

	const n = 40
	var wg sync.WaitGroup
	wg.Add(2)

	// Produtor: cria pedidos e os marca em curso, na mesma ordem que o servidor faz.
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			id := "novo-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
			pedidoEmDisco(t, s.baseDir, id)
			registrarEmCurso(s, id, pipeline.EstadoBaixandoVideo)
		}
	}()

	// Limpador: roda a política em paralelo, sem parar.
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			s.limparSobLock()
		}
	}()
	wg.Wait()

	// Todo pedido em curso precisa ter sobrevivido inteiro.
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, reg := range s.pedidos {
		if estadoTerminal(reg.ped.Status) {
			continue
		}
		if _, err := os.Stat(filepath.Join(s.baseDir, id, "video.mp4")); err != nil {
			t.Fatalf("limpeza concorrente apagou o vídeo do pedido em curso %s", id)
		}
	}
}
