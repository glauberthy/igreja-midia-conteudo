package servidor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"srtclean/internal/harness"
	"srtclean/internal/pipeline"
	"srtclean/internal/validacao"
)

func TestTextoDoTrecho(t *testing.T) {
	frases := harness.Frasear(strings.Join([]string{
		"[00:00:00] Antes da janela do trecho.",
		"[00:00:10] A graça de Deus é suficiente.",
		"[00:00:20] Ele sustenta o fraco todo dia.",
		"[00:00:40] Fora da janela, depois do fim.",
	}, "\n"))
	got := textoDoTrecho(frases, "00:00:10.000", "00:00:35.000")
	if !strings.Contains(got, "A graça de Deus") || !strings.Contains(got, "sustenta o fraco") {
		t.Errorf("texto do trecho não juntou as frases da janela: %q", got)
	}
	if strings.Contains(got, "Antes da janela") || strings.Contains(got, "Fora da janela") {
		t.Errorf("texto do trecho vazou frases fora da janela: %q", got)
	}
}

func TestRevisaoJSONShape(t *testing.T) {
	reg := &registro{
		ped: pipeline.NovoPedido("ped-1", "https://youtu.be/cultoTeste1", "00:00:00", "00:10:00", time.Unix(0, 0)),
		cands: []validacao.Candidato{
			{Hook: "Hook A", Start: "00:01:00.000", End: "00:01:35.000", DurationSeconds: 35, Score: 88},
			{Hook: "Hook B", Start: "00:02:00.000", End: "00:02:34.000", DurationSeconds: 34, Score: 70,
				RequerRevisaoReforcada: true, MotivoRevisao: "possível problema de fidelidade — revisar"},
		},
		textos: []string{"Texto falado A completo.", "Texto falado B completo."},
	}
	var d dadosRevisao
	if err := json.Unmarshal([]byte(revisaoJSON(reg, false)), &d); err != nil {
		t.Fatalf("payload de revisão não é JSON válido: %v", err)
	}
	if d.PedidoID != "ped-1" || d.VideoID != "cultoTeste1" {
		t.Errorf("payload sem contexto: %+v", d)
	}
	if len(d.Trechos) != 2 {
		t.Fatalf("esperava 2 trechos, veio %d", len(d.Trechos))
	}
	a, b := d.Trechos[0], d.Trechos[1]
	if a.Texto != "Texto falado A completo." || a.Inicio != "00:01:00" || a.StartSeg != 60 || a.Score != 88 {
		t.Errorf("trecho A inesperado: %+v", a)
	}
	if !b.Revisar || b.Motivo == "" || b.EndSeg != 154 {
		t.Errorf("trecho B (marcado) inesperado: %+v", b)
	}
}

// A revisão ordena os trechos CRONOLOGICAMENTE (não por score), preservando o índice
// original (que o /aprovar usa) e marcando o de maior score com um selo — decisão da
// spec-05 (ordenar por score empurraria os marcados para o fim).
func TestRevisaoOrdemCronologicaPreservaIndice(t *testing.T) {
	reg := &registro{
		ped: pipeline.NovoPedido("p", "https://youtu.be/cultoTeste1", "00:00:00", "00:10:00", time.Unix(0, 0)),
		cands: []validacao.Candidato{
			// fora de ordem cronológica; o de MAIOR score (índice 0) é o mais tardio.
			{Hook: "tardio", Start: "00:05:00.000", End: "00:05:35.000", DurationSeconds: 35, Score: 95},
			{Hook: "cedo", Start: "00:01:00.000", End: "00:01:34.000", DurationSeconds: 34, Score: 70},
			{Hook: "meio", Start: "00:03:00.000", End: "00:03:33.000", DurationSeconds: 33, Score: 80},
		},
		textos: []string{"", "", ""},
	}
	var d dadosRevisao
	if err := json.Unmarshal([]byte(revisaoJSON(reg, false)), &d); err != nil {
		t.Fatal(err)
	}
	// Ordem de exibição = cronológica: cedo, meio, tardio.
	if d.Trechos[0].Hook != "cedo" || d.Trechos[1].Hook != "meio" || d.Trechos[2].Hook != "tardio" {
		t.Errorf("ordem não é cronológica: %s, %s, %s", d.Trechos[0].Hook, d.Trechos[1].Hook, d.Trechos[2].Hook)
	}
	// Índices originais preservados (o /aprovar usa estes): cedo=1, meio=2, tardio=0.
	if d.Trechos[0].Indice != 1 || d.Trechos[1].Indice != 2 || d.Trechos[2].Indice != 0 {
		t.Errorf("índices originais não preservados: %d, %d, %d", d.Trechos[0].Indice, d.Trechos[1].Indice, d.Trechos[2].Indice)
	}
	// Selo de maior score no "tardio" (score 95), não no primeiro exibido.
	if d.Trechos[0].MelhorScore || !d.Trechos[2].MelhorScore {
		t.Errorf("selo de maior score no trecho errado: %+v", d.Trechos)
	}
}

// Integração: com uma transcrição real, a fase leve computa o texto falado e ele chega
// no payload de revisão (o artefato principal para julgar doutrina).
func TestFaseLeveIncluiTextoFalado(t *testing.T) {
	transc := strings.Join([]string{
		"[00:01:00] A graça de Deus confronta o legalismo hoje.",
		"[00:01:20] E o coração precisa ser quebrantado diante dele.",
	}, "\n")
	sel := &selecionadorFake{cands: []validacao.Candidato{
		{Hook: "A graça de Deus confronta o legalismo hoje.", Start: "00:01:00.000", End: "00:01:35.000", DurationSeconds: 35, Score: 88},
	}}
	s := servidorTeste(t, &baixadorFake{transc: transc}, sel)
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)

	req := httptest.NewRequest("GET", "/pedidos/teste-1", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	corpo := rec.Body.String()
	if !strings.Contains(corpo, "coração precisa ser quebrantado") {
		t.Errorf("texto falado (da janela) não chegou ao payload de revisão:\n%s", corpo)
	}
}

func TestAssetsServeArquivo(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "fontes", "static"), 0755)
	os.WriteFile(filepath.Join(dir, "fontes", "static", "x.ttf"), []byte("FONTE-FAKE"), 0644)
	s := Novo(Opcoes{Baixador: &baixadorFake{}, Selecionador: &selecionadorFake{}, AssetsDir: dir})

	req := httptest.NewRequest("GET", "/assets/fontes/static/x.ttf", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "FONTE-FAKE") {
		t.Errorf("rota /assets não serviu o arquivo: código %d, corpo %q", rec.Code, rec.Body.String())
	}
}
