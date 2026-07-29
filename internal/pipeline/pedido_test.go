package pipeline

import (
	"testing"
	"time"
)

func TestPedidoSalvarCarregar(t *testing.T) {
	base := t.TempDir()
	criado := time.Date(2026, 7, 21, 10, 30, 0, 0, time.UTC)

	p := NovoPedido("abc123", "https://youtu.be/xyz", "00:05:00", "00:40:00", criado)
	p.Status = EstadoConcluido

	if err := p.Salvar(base); err != nil {
		t.Fatalf("Salvar: %v", err)
	}

	got, err := Carregar(base, "abc123")
	if err != nil {
		t.Fatalf("Carregar: %v", err)
	}

	if got.ID != p.ID || got.YouTubeURL != p.YouTubeURL || got.Inicio != p.Inicio || got.Fim != p.Fim {
		t.Errorf("campos básicos não bateram: %+v", got)
	}
	if got.Status != EstadoConcluido {
		t.Errorf("status = %q, queria %q", got.Status, EstadoConcluido)
	}
	if !got.CriadoEm.Equal(criado) {
		t.Errorf("CriadoEm = %v, queria %v", got.CriadoEm, criado)
	}
}

// A origem de tempo do video.mp4 é o dado cuja AUSÊNCIA tem significado próprio, distinto do
// valor zero (vídeo inteiro). Se o JSON não preservar essa distinção, o render volta a não
// saber se lê um fato ou um default — a raiz do bug de cena errada (ver spec-09).
func TestOrigemDeclaradaSobreviveAoJSON(t *testing.T) {
	base := t.TempDir()

	// (1) ZERO declarado tem de voltar como zero declarado, não como "ausente". É o caso do
	// servidor (vídeo inteiro), justamente o que `omitempty` apagaria num int comum.
	inteiro := NovoPedido("inteiro", "url", "00:49:15", "01:24:30", time.Unix(0, 0).UTC())
	inteiro.DeclararOrigem(0)
	if err := inteiro.Salvar(base); err != nil {
		t.Fatal(err)
	}
	lido, err := Carregar(base, "inteiro")
	if err != nil {
		t.Fatal(err)
	}
	origem, err := lido.Origem()
	if err != nil {
		t.Fatalf("origem 0 desapareceu no JSON (virou ausente): %v", err)
	}
	if origem != 0 {
		t.Errorf("origem = %d, queria 0", origem)
	}
	// E o Inicio da pregação continua lá, separado: são dois dados, não um.
	if lido.Inicio != "00:49:15" {
		t.Errorf("Inicio = %q, queria 00:49:15", lido.Inicio)
	}

	// (2) Janela: a origem é o início da janela, diferente de zero.
	janela := NovoPedido("janela", "url", "00:05:30", "00:38:10", time.Unix(0, 0).UTC())
	janela.DeclararOrigem(330000)
	if err := janela.Salvar(base); err != nil {
		t.Fatal(err)
	}
	if lidoJ, err := Carregar(base, "janela"); err != nil {
		t.Fatal(err)
	} else if o, err := lidoJ.Origem(); err != nil || o != 330000 {
		t.Errorf("origem da janela = %d (err %v), queria 330000", o, err)
	}

	// (3) AUSENTE continua ausente: pedido antigo não ganha origem por acidente.
	antigo := NovoPedido("antigo", "url", "00:05:30", "00:38:10", time.Unix(0, 0).UTC())
	if err := antigo.Salvar(base); err != nil {
		t.Fatal(err)
	}
	lidoA, err := Carregar(base, "antigo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lidoA.Origem(); err == nil {
		t.Error("pedido sem declaração devolveu origem: a ausência tem de continuar sendo ausência")
	}
}

func TestNovoPedidoEstadoInicial(t *testing.T) {
	p := NovoPedido("id1", "url", "", "", time.Unix(0, 0).UTC())
	if p.Status != EstadoRecebido {
		t.Errorf("estado inicial = %q, queria %q", p.Status, EstadoRecebido)
	}
}

func TestSalvarSemID(t *testing.T) {
	p := &Pedido{}
	if err := p.Salvar(t.TempDir()); err == nil {
		t.Error("esperava erro ao salvar pedido sem ID")
	}
}
