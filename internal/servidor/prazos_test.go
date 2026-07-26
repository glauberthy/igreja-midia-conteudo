package servidor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"srtclean/internal/pipeline"
	"srtclean/internal/validacao"
)

// baixadorVideoTravado simula o pior caso real: o yt-dlp que não sai (stall de rede). Não
// devolve erro, não progride — só espera o ctx ser cancelado.
type baixadorVideoTravado struct{ base string }

func (b *baixadorVideoTravado) BaixarVideoCompleto(ctx context.Context, ped *pipeline.Pedido) error {
	// Deixa no disco o mp4 parcial que um download travado deixaria.
	dir := filepath.Join(b.base, ped.ID)
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "video.mp4"), make([]byte, 8192), 0644)
	<-ctx.Done()
	return ctx.Err()
}

// selecionadorTravado é o equivalente para o modelo que para de responder.
type selecionadorTravado struct{}

func (s *selecionadorTravado) Selecionar(ctx context.Context, _ string) ([]validacao.Candidato, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func servidorComPrazos(t *testing.T, p Prazos, bv BaixadorVideo, sel Selecionador) *Servidor {
	t.Helper()
	base := t.TempDir()
	out := t.TempDir()
	rf := &renderFake{outDir: out}
	if sel == nil {
		sel = candsJanela()
	}
	return Novo(Opcoes{
		Baixador:     &baixadorFake{transc: "[00:00:00] a graça basta.", base: base},
		Selecionador: sel, BaixadorVideo: bv, Renderizador: rf,
		BaseDir: base, OutDir: out,
		LogRodadasPath: filepath.Join(base, "rodadas.md"),
		TemposPath:     filepath.Join(base, "tempos.csv"),
		Prazos:         p,
		Agora:          func() time.Time { return time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC) },
		GerarID:        func() string { return "teste-1" },
	})
}

// TestDownloadTravadoTerminaEmErro é a garantia de que o estado SEMPRE termina. Sem prazo,
// este pedido ficaria em "baixando-video" para sempre: spinner infinito para o operador,
// fila bloqueada, e ~900 MB permanentemente protegidos da limpeza.
func TestDownloadTravadoTerminaEmErro(t *testing.T) {
	s := servidorComPrazos(t, Prazos{Video: 60 * time.Millisecond}, nil, nil)
	s.baixadorVideo = &baixadorVideoTravado{base: s.baseDir}

	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, "teste-1", []int{0})
	esperarStatus(t, s, "teste-1", pipeline.EstadoErro)

	s.mu.Lock()
	msg := s.pedidos["teste-1"].ped.Erro
	s.mu.Unlock()
	for _, quer := range []string{"download do vídeo", "interrompid"} {
		if !strings.Contains(msg, quer) {
			t.Errorf("mensagem não nomeia o problema (%q): %q", quer, msg)
		}
	}
}

// TestPedidoTravadoNaoFicaImortal fecha o ciclo com a spec-06: o prazo transforma o pedido
// travado em terminal, e só por isso o material bruto dele volta a ser limpável. Sem
// prazo, a proteção de "pedido em curso" viraria vazamento permanente de disco.
func TestPedidoTravadoNaoFicaImortal(t *testing.T) {
	s := servidorComPrazos(t, Prazos{Video: 60 * time.Millisecond}, nil, nil)
	s.baixadorVideo = &baixadorVideoTravado{base: s.baseDir}
	s.reterPedidos = 1

	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, "teste-1", []int{0})
	esperarStatus(t, s, "teste-1", pipeline.EstadoErro)

	// O resíduo do download travado é apagado na hora (limparResiduoDeErro).
	esperarSumir(t, filepath.Join(s.baseDir, "teste-1", "video.mp4"))

	// E o pedido deixou de ser intocável para a política de retenção.
	s.mu.Lock()
	intocaveis := s.intocaveisLocked()
	s.mu.Unlock()
	for _, id := range intocaveis {
		if id == "teste-1" {
			t.Fatal("pedido travado continua protegido da limpeza após o prazo estourar")
		}
	}
}

// TestSelecaoTravadaTerminaEmErro: o mesmo para o modelo que para de responder.
func TestSelecaoTravadaTerminaEmErro(t *testing.T) {
	s := servidorComPrazos(t, Prazos{Selecao: 60 * time.Millisecond}, &baixadorVideoFake{}, &selecionadorTravado{})

	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoErro)

	s.mu.Lock()
	msg := s.pedidos["teste-1"].ped.Erro
	s.mu.Unlock()
	if !strings.Contains(msg, "seleção dos trechos") {
		t.Errorf("mensagem não nomeia a etapa: %q", msg)
	}
	// A mensagem de prazo já se explica; não pode vir prefixada com "falha na seleção:".
	if strings.HasPrefix(msg, "falha na seleção:") {
		t.Errorf("mensagem de prazo foi prefixada, ficou redundante: %q", msg)
	}
}

// TestPrazosPadraoSaoFolgados protege contra alguém apertar os prazos a ponto de
// interromper trabalho legítimo. Referência medida: download ~86s, render ~3s/Short.
func TestPrazosPadraoSaoFolgados(t *testing.T) {
	p := PrazosPadrao()
	if p.Video < 15*time.Minute {
		t.Errorf("prazo do vídeo curto demais (%s): 900 MB em rede lenta precisam de folga", p.Video)
	}
	if p.Selecao < 20*time.Minute {
		t.Errorf("prazo da seleção curto demais (%s): o harness faz várias chamadas", p.Selecao)
	}
}

// TestPrazoZeradoUsaPadrao: quem configura só um campo não perde os outros.
func TestPrazoZeradoUsaPadrao(t *testing.T) {
	p := Prazos{Video: time.Second}.comPadroes()
	if p.Video != time.Second {
		t.Error("campo explícito foi sobrescrito")
	}
	if p.Selecao != PrazosPadrao().Selecao || p.Legenda != PrazosPadrao().Legenda {
		t.Error("campo zerado não recebeu o padrão")
	}
}

// TestEtapaComPrazoNaoMascaraErroReal: erro comum passa intacto, sem virar "prazo".
func TestEtapaComPrazoNaoMascaraErroReal(t *testing.T) {
	real := errors.New("vídeo indisponível")
	err := etapaComPrazo(context.Background(), "o download", time.Minute, func(context.Context) error {
		return real
	})
	if !errors.Is(err, real) {
		t.Fatalf("erro real foi trocado: %v", err)
	}
	if errors.Is(err, ErrPrazoEstourado) {
		t.Error("erro real classificado como estouro de prazo")
	}
}
