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

func (b *baixadorVideoTravado) BaixarVideoCompleto(ctx context.Context, ped *pipeline.Pedido, dirDestino string) (int, error) {
	// Deixa no disco o mp4 parcial que um download travado deixaria.
	dir := dirDestino
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "video.mp4"), make([]byte, 8192), 0644)
	<-ctx.Done()
	return 0, ctx.Err()
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
		CortesPath:     filepath.Join(base, "cortes.csv"),
		Prazos:         p,
		Agora:          func() time.Time { return time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC) },
		GerarID:        func() string { return "teste-1" },
	})
}

// TestDownloadTravadoTerminaEmErro é a garantia de que o estado SEMPRE termina. Sem prazo,
// este pedido ficaria em "baixando-video" para sempre: spinner infinito para o operador,
// fila bloqueada, e ~900 MB permanentemente protegidos da limpeza.
func TestDownloadTravadoTerminaEmErro(t *testing.T) {
	s := servidorComPrazos(t, Prazos{VideoSemProgresso: 250 * time.Millisecond}, nil, nil)
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
	s := servidorComPrazos(t, Prazos{VideoSemProgresso: 250 * time.Millisecond}, nil, nil)
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
	intocaveis, _ := s.emCursoLocked()
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
	// O download não tem teto de tempo: o pior caso é tamanho x throughput, ambos muito
	// variáveis. O que se exige é que o watchdog dê folga a uma rede lenta mas viva.
	if p.VideoSemProgresso < 3*time.Minute {
		t.Errorf("watchdog do vídeo agressivo demais (%s): uma pausa curta do yt-dlp entre "+
			"fragmentos mataria um download saudável", p.VideoSemProgresso)
	}
	if p.VideoTeto < time.Hour {
		t.Errorf("teto do vídeo curto demais (%s): 1,8 GB a 3,3 MB/s levam ~9min, e ele é só "+
			"rede de segurança", p.VideoTeto)
	}
	if p.Selecao < 20*time.Minute {
		t.Errorf("prazo da seleção curto demais (%s): o harness faz várias chamadas", p.Selecao)
	}
}

// TestPrazoZeradoUsaPadrao: quem configura só um campo não perde os outros.
func TestPrazoZeradoUsaPadrao(t *testing.T) {
	p := Prazos{VideoSemProgresso: time.Second}.comPadroes()
	if p.VideoSemProgresso != time.Second {
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

// baixadorVideoLento escreve devagar mas SEM PARAR, e termina. Representa o culto de 2h
// em rede ruim: o caso que um teto de tempo fixo mataria injustamente.
type baixadorVideoLento struct {
	base      string
	pedacos   int
	intervalo time.Duration
}

func (b *baixadorVideoLento) BaixarVideoCompleto(ctx context.Context, ped *pipeline.Pedido, dirDestino string) (int, error) {
	dir := dirDestino
	os.MkdirAll(dir, 0755)
	f, err := os.Create(filepath.Join(dir, "video.mp4"))
	if err != nil {
		return 0, err
	}
	defer f.Close()
	for i := 0; i < b.pedacos; i++ {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(b.intervalo):
		}
		if _, err := f.Write(make([]byte, 2<<20)); err != nil {
			return 0, err
		}
		f.Sync()
	}
	return 0, nil
}

// TestDownloadLentoMasVivoNaoEhMorto é o outro lado do watchdog, e o motivo de ele existir:
// um download que progride devagar precisa CHEGAR AO FIM. Aqui ele leva bem mais do que a
// janela sem-progresso no total, e ainda assim não pode ser interrompido.
func TestDownloadLentoMasVivoNaoEhMorto(t *testing.T) {
	s := servidorComPrazos(t, Prazos{VideoSemProgresso: 200 * time.Millisecond}, nil, nil)
	// 12 escritas de 2 MB a cada 60ms: ~720ms totais (3,6x a janela sem-progresso) e 24 MB, que
	// passa do mínimo para o arquivo contar como vídeo utilizável.
	s.baixadorVideo = &baixadorVideoLento{base: s.baseDir, pedacos: 12, intervalo: 60 * time.Millisecond}

	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, "teste-1", []int{0})
	esperarStatus(t, s, "teste-1", pipeline.EstadoConcluido)
}

// TestWatchdogNomeiaFaltaDeProgresso: a mensagem tem de dizer que travou, não que "passou
// do tempo" — são diagnósticos diferentes para o operador.
func TestWatchdogNomeiaFaltaDeProgresso(t *testing.T) {
	s := servidorComPrazos(t, Prazos{VideoSemProgresso: 200 * time.Millisecond}, nil, nil)
	s.baixadorVideo = &baixadorVideoTravado{base: s.baseDir}

	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, "teste-1", []int{0})
	esperarStatus(t, s, "teste-1", pipeline.EstadoErro)

	s.mu.Lock()
	msg := s.pedidos["teste-1"].ped.Erro
	s.mu.Unlock()
	if !strings.Contains(msg, "sem baixar nada") {
		t.Errorf("mensagem não diz que travou por falta de progresso: %q", msg)
	}
}

// TestTetoAbsolutoDoVideo: a rede de segurança funciona mesmo com progresso constante
// (yt-dlp patológico escrevendo lixo para sempre).
func TestTetoAbsolutoDoVideo(t *testing.T) {
	s := servidorComPrazos(t, Prazos{
		VideoSemProgresso: time.Hour,              // watchdog nunca dispara
		VideoTeto:         300 * time.Millisecond, // só o teto pode agir
	}, nil, nil)
	s.baixadorVideo = &baixadorVideoLento{base: s.baseDir, pedacos: 10000, intervalo: 10 * time.Millisecond}

	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, "teste-1", []int{0})
	esperarStatus(t, s, "teste-1", pipeline.EstadoErro)

	s.mu.Lock()
	msg := s.pedidos["teste-1"].ped.Erro
	s.mu.Unlock()
	if !strings.Contains(msg, "passou de") {
		t.Errorf("mensagem não indica o teto absoluto: %q", msg)
	}
}
