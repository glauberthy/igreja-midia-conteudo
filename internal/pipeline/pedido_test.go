package pipeline

import (
	"testing"
	"time"

	"srtclean/internal/validacao"
)

func TestPedidoSalvarCarregar(t *testing.T) {
	base := t.TempDir()
	criado := time.Date(2026, 7, 21, 10, 30, 0, 0, time.UTC)

	p := NovoPedido("abc123", "https://youtu.be/xyz", "00:05:00", "00:40:00", criado)
	p.Status = EstadoConcluido
	p.Candidatos = []validacao.Candidato{
		{Start: "00:00:11.000", End: "00:00:30.000", DurationSeconds: 19, Score: 100, Hook: "de verdade eu vos digo"},
	}

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
	if len(got.Candidatos) != 1 || got.Candidatos[0].Hook != "de verdade eu vos digo" {
		t.Errorf("candidatos não preservados: %+v", got.Candidatos)
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
