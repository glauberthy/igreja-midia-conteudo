package servidor

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"srtclean/internal/pipeline"
	"srtclean/internal/validacao"
)

// --- Mocks da fase pesada ---

type baixadorVideoFake struct {
	erro        error
	inicio, fim string
	chamado     bool
	mu          sync.Mutex
}

func (b *baixadorVideoFake) BaixarVideoJanela(ctx context.Context, ped *pipeline.Pedido, inicio, fim string) error {
	b.mu.Lock()
	b.inicio, b.fim, b.chamado = inicio, fim, true
	b.mu.Unlock()
	if b.erro != nil {
		ped.Status = pipeline.EstadoErro
		ped.Erro = b.erro.Error()
		return b.erro
	}
	return nil
}

type renderFake struct {
	erro     error
	outDir   string
	origemMs int
	nCands   int
	mu       sync.Mutex
}

func (r *renderFake) RenderizarComOrigem(ctx context.Context, ped *pipeline.Pedido, cands []validacao.Candidato, origemMs int) ([]string, error) {
	r.mu.Lock()
	r.origemMs, r.nCands = origemMs, len(cands)
	r.mu.Unlock()
	if r.erro != nil {
		return nil, r.erro
	}
	dir := filepath.Join(r.outDir, ped.ID)
	os.MkdirAll(dir, 0755)
	var paths []string
	for i := range cands {
		p := filepath.Join(dir, fmt.Sprintf("short_%02d.mp4", i+1))
		os.WriteFile(p, []byte("mp4 fake"), 0644)
		paths = append(paths, p)
	}
	return paths, nil
}

func servidorPesada(t *testing.T, sel *selecionadorFake, bv *baixadorVideoFake, rf *renderFake) *Servidor {
	t.Helper()
	base := t.TempDir()
	out := t.TempDir()
	rf.outDir = out
	return Novo(Opcoes{
		Baixador:      &baixadorFake{transc: "[00:00:00] a graça basta.", base: base},
		Selecionador:  sel,
		BaixadorVideo: bv,
		Renderizador:  rf,
		BaseDir:       base,
		OutDir:        out,
		Agora:         func() time.Time { return time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC) },
		GerarID:       func() string { return "teste-1" },
	})
}

func candsJanela() *selecionadorFake {
	return &selecionadorFake{cands: []validacao.Candidato{
		{Hook: "trecho 0", Start: "01:45:25.800", End: "01:46:10.200", DurationSeconds: 44, Score: 88},
		{Hook: "trecho 1", Start: "00:50:30.000", End: "00:51:00.000", DurationSeconds: 30, Score: 92},
		{Hook: "trecho 2", Start: "02:04:03.000", End: "02:04:37.500", DurationSeconds: 34, Score: 77},
	}}
}

// TestJanelaDownload: a janela contígua vai do menor start (piso ao seg) ao maior end
// (teto ao seg); a origem é o piso do menor start. É a correspondência de tempo do lado
// do servidor.
func TestJanelaDownload(t *testing.T) {
	aprovados := []validacao.Candidato{
		{Start: "01:45:25.800", End: "01:46:10.200"}, // menor start
		{Start: "02:04:03.000", End: "02:04:37.500"}, // maior end
	}
	ini, fim, origem := janelaDownload(aprovados)
	if ini != "01:45:25" {
		t.Errorf("início = %q, quero 01:45:25 (piso ao segundo do menor start)", ini)
	}
	if fim != "02:04:38" {
		t.Errorf("fim = %q, quero 02:04:38 (teto ao segundo do maior end)", fim)
	}
	if origem != 6325000 {
		t.Errorf("origem = %d ms, quero 6325000 (= 01:45:25)", origem)
	}
}

// Fluxo completo: aprovar dispara a fase pesada; download recebe a janela contígua, render
// recebe a origem certa, e os Shorts ficam disponíveis para download.
func TestFaseHeavyFluxoCompleto(t *testing.T) {
	bv := &baixadorVideoFake{}
	rf := &renderFake{}
	s := servidorPesada(t, candsJanela(), bv, rf)
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)

	// Aprova os trechos 0 (01:45) e 2 (02:04).
	aprovarJSON(t, s, "teste-1", []int{0, 2})
	esperarStatus(t, s, "teste-1", pipeline.EstadoConcluido)

	// Download da janela contígua [01:45:25, 02:04:38].
	bv.mu.Lock()
	ini, fim := bv.inicio, bv.fim
	bv.mu.Unlock()
	if ini != "01:45:25" || fim != "02:04:38" {
		t.Errorf("janela de download = [%s, %s], quero [01:45:25, 02:04:38]", ini, fim)
	}
	// Render com a origem = menor start aprovado (01:45:25 = 6325000 ms), 2 candidatos.
	rf.mu.Lock()
	origem, n := rf.origemMs, rf.nCands
	rf.mu.Unlock()
	if origem != 6325000 {
		t.Errorf("origem do render = %d, quero 6325000 (o corte seria no lugar errado)", origem)
	}
	if n != 2 {
		t.Errorf("render recebeu %d candidatos, quero 2 (os aprovados)", n)
	}

	// Os Shorts aparecem na visão e são baixáveis.
	vis := statusJSONDoPedido(t, s, "teste-1")
	if vis.Status != string(pipeline.EstadoConcluido) {
		t.Errorf("status final = %q, quero concluido", vis.Status)
	}
	req := httptest.NewRequest("GET", "/finalizados/teste-1/short_01.mp4", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
		t.Errorf("download do short_01 falhou: código %d, %d bytes", rec.Code, rec.Body.Len())
	}
}

func TestFaseHeavyErroDownloadNaoTrava(t *testing.T) {
	bv := &baixadorVideoFake{erro: fmt.Errorf("googlevideo timeout")}
	rf := &renderFake{}
	s := servidorPesada(t, candsJanela(), bv, rf)
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, "teste-1", []int{0})
	esperarStatus(t, s, "teste-1", pipeline.EstadoErro) // erro visível, não spinner infinito

	vis := statusJSONDoPedido(t, s, "teste-1")
	if vis.Erro == "" {
		t.Error("falha no download deveria dar erro com mensagem, não travar")
	}
	rf.mu.Lock()
	renderChamado := rf.nCands
	rf.mu.Unlock()
	if renderChamado != 0 {
		t.Error("não deveria renderizar se o download falhou")
	}
}

func TestFaseHeavyErroRenderNaoTrava(t *testing.T) {
	bv := &baixadorVideoFake{}
	rf := &renderFake{erro: fmt.Errorf("ffmpeg: Invalid data")}
	s := servidorPesada(t, candsJanela(), bv, rf)
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, "teste-1", []int{0})
	esperarStatus(t, s, "teste-1", pipeline.EstadoErro)

	if vis := statusJSONDoPedido(t, s, "teste-1"); vis.Erro == "" {
		t.Error("falha no render deveria dar erro com mensagem")
	}
}

// O download só serve arquivos que o pedido gerou (whitelist), nunca um caminho arbitrário.
func TestBaixarFinalRecusaArquivoForaDaWhitelist(t *testing.T) {
	bv := &baixadorVideoFake{}
	rf := &renderFake{}
	s := servidorPesada(t, candsJanela(), bv, rf)
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)
	aprovarJSON(t, s, "teste-1", []int{0})
	esperarStatus(t, s, "teste-1", pipeline.EstadoConcluido)

	// Um arquivo que o pedido não gerou → 404 (não vaza nem permite travessia).
	req := httptest.NewRequest("GET", "/finalizados/teste-1/segredo.txt", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("arquivo fora da whitelist deveria dar 404, veio %d", rec.Code)
	}
}
