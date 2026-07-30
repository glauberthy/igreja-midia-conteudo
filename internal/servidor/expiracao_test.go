package servidor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"srtclean/internal/pipeline"
	"srtclean/internal/videocache"
)

// cultoVelhoNoCache põe um culto ANTIGO no cache do servidor, como um pedido de semanas atrás
// teria deixado.
func cultoVelhoNoCache(t *testing.T, s *Servidor, videoID string, usadoEm time.Time) string {
	t.Helper()
	dir, err := s.cache.DirVideo(videoID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := escreverVideoFalso(filepath.Join(dir, videocache.NomeVideo)); err != nil {
		t.Fatal(err)
	}
	if err := s.cache.Registrar(videoID, videocache.OrigemVideoInteiro, "Culto antigo"); err != nil {
		t.Fatal(err)
	}
	// Envelhece o último uso reescrevendo o video.json — o mesmo arquivo que a produção grava.
	idx, err := s.cache.LerIndice(videoID)
	if err != nil {
		t.Fatal(err)
	}
	idx.UsadoEm = usadoEm
	b, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, videocache.NomeIndice), b, 0644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, videocache.NomeVideo)
}

// TestExpiracaoDoCacheRodaNoCaminhoDoOperador é o teste que impede a política de existir sem ser
// usada — o erro que este projeto já pagou três vezes (constante declarada num lugar, caminho
// real lendo outro). Aqui a expiração tem de acontecer no ciclo NORMAL do operador: ele aprova,
// o Short sai, e o disco encolhe.
//
// O ciclo passa por DOIS pontos de expiração (garantirEspaco, antes do download; limparSobLock,
// depois de concluir), e este teste mede o efeito, não qual dos dois agiu — quem isola o segundo
// é o TestVideoDePedidoEmCursoNaoExpira.
//
// E, no mesmo passe, verifica a invariante caríssima: o vídeo do culto que ESTE pedido acabou de
// usar não pode ser tocado, mesmo com o teto estourado. O teto aqui é 1 byte — quem sobra, sobra
// por estar protegido, não por caber.
func TestExpiracaoDoCacheRodaNoCaminhoDoOperador(t *testing.T) {
	bv := &baixadorVideoFake{}
	s := servidorComCache(t, candsJanela(), bv, &renderFake{})
	s.videoTeto = 1 // nada cabe: só escapa o que é intocável

	velho := cultoVelhoNoCache(t, s, "cultoAntigo1", s.agora().Add(-40*24*time.Hour))

	id := criarPedido(t, s, "https://youtu.be/cultoTeste1", "00:00:00", "00:10:00")
	esperarStatus(t, s, id, pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, id, []int{0})
	esperarStatus(t, s, id, pipeline.EstadoConcluido)

	// O culto antigo perdeu o vídeo (é o ganho: ~570 MB por culto em produção).
	esperarSumir(t, velho)

	// O culto DESTE pedido continua inteiro. Sem a proteção, o teto de 1 byte comeria os dois —
	// e o operador que clicasse em "gerar de novo" pagaria um download de 570 MB por causa de
	// uma limpeza que rodou meio segundo antes.
	doPedido, err := s.cache.DirVideo("cultoTeste1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(doPedido, videocache.NomeVideo)); err != nil {
		t.Fatalf("a expiração apagou o vídeo do culto que o pedido acabou de usar: %v", err)
	}
	// E a legenda do culto expirado ficou: o próximo pedido dele baixa vídeo, não legenda.
	if !s.cache.TemLegenda("cultoTeste1") {
		t.Error("a legenda do culto em uso desapareceu")
	}
}

// TestVideoDePedidoEmCursoNaoExpira: o caso mais grave, porque acontece DURANTE o trabalho — a
// fase pesada está renderizando quando a limpeza de outro pedido roda. O pedido em curso não é
// "extra" de ninguém: ele entra na lista por não estar em estado terminal.
//
// Este teste chama limparSobLock DIRETO, e é por isso que ele também é o que prova a ligação
// naquele caminho: o culto antigo tem de sumir na mesma chamada. Sem esta asserção, tirar a
// expiração de dentro da limpeza passaria em silêncio — o teste de ponta a ponta continuaria
// verde porque o garantirEspaco também expira, antes do download.
func TestVideoDePedidoEmCursoNaoExpira(t *testing.T) {
	bv := &baixadorVideoFake{}
	s := servidorComCache(t, candsJanela(), bv, &renderFake{})
	s.videoTeto = 1

	id := criarPedido(t, s, "https://youtu.be/cultoTeste1", "00:00:00", "00:10:00")
	esperarStatus(t, s, id, pipeline.EstadoAguardandoAprovacao) // em curso: esperando o operador

	// O vídeo entra no cache antes da aprovação (como se outro pedido o tivesse baixado).
	cultoVelhoNoCache(t, s, "cultoTeste1", s.agora().Add(-90*24*time.Hour))
	velho := cultoVelhoNoCache(t, s, "cultoAntigo1", s.agora().Add(-90*24*time.Hour))

	// Limpeza disparada por OUTRO motivo, sem extras: só a lista de "em curso" protege.
	s.limparSobLock()

	if _, err := os.Stat(velho); err == nil {
		t.Error("a limpeza do servidor não expirou o cache: o culto de 90 dias sem uso ficou em disco")
	}
	dir, _ := s.cache.DirVideo("cultoTeste1")
	if _, err := os.Stat(filepath.Join(dir, videocache.NomeVideo)); err != nil {
		t.Fatalf("a expiração apagou o vídeo de um pedido AGUARDANDO APROVAÇÃO: %v", err)
	}
}

// TestEmCursoProtegePedidoEVideoJuntos trava a projeção: as duas listas saem do mesmo passe, e um
// pedido em curso protege as duas unidades que a limpeza apaga. Se um dia alguém devolver só os
// pedidos, este teste diz o que se perdeu.
func TestEmCursoProtegePedidoEVideoJuntos(t *testing.T) {
	bv := &baixadorVideoFake{}
	s := servidorComCache(t, candsJanela(), bv, &renderFake{})
	id := criarPedido(t, s, "https://youtu.be/cultoTeste1", "00:00:00", "00:10:00")
	esperarStatus(t, s, id, pipeline.EstadoAguardandoAprovacao)

	s.mu.Lock()
	pedidos, videos := s.emCursoLocked()
	s.mu.Unlock()

	if len(pedidos) != 1 || pedidos[0] != id {
		t.Errorf("pedidos em curso = %v, quero [%s]", pedidos, id)
	}
	if len(videos) != 1 || videos[0] != "cultoTeste1" {
		t.Errorf("vídeos em curso = %v, quero [cultoTeste1]: sem isto a expiração do cache "+
			"apagaria o vídeo de um pedido que a limpeza de pedidos considera intocável", videos)
	}
}

// TestGarantirEspacoExpiraOCacheAntesDeBaixar cobre o segundo ponto de ligação, que é o que
// importa quando o disco APERTA: é ali, com um download de ~570 MB a uma linha de começar, que a
// pressão é real — e é o cache que tem os GB. Sem esta chamada, o GarantirEspaco varreria
// trabalho/, onde cada pedido hoje ocupa KB, e falharia dizendo "não há espaço" com 50 GB de
// culto velho parado ao lado.
//
// Sem este teste a ligação era invisível: tirá-la do código não fazia nenhum teste falhar (achado
// por mutação, não por leitura).
func TestGarantirEspacoExpiraOCacheAntesDeBaixar(t *testing.T) {
	s := servidorComCache(t, candsJanela(), &baixadorVideoFake{}, &renderFake{})
	velho := cultoVelhoNoCache(t, s, "cultoAntigo1", s.agora().Add(-40*24*time.Hour))

	if err := s.garantirEspaco("pedido-inexistente"); err != nil {
		t.Fatalf("garantirEspaco falhou: %v", err)
	}
	if _, err := os.Stat(velho); err == nil {
		t.Error("o culto de 40 dias sem uso sobreviveu à verificação de espaço da fase pesada")
	}
}
