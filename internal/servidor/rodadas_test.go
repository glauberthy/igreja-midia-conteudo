package servidor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"srtclean/internal/pipeline"
	"srtclean/internal/validacao"
)

func servidorComLog(t *testing.T, logPath string) *Servidor {
	t.Helper()
	return Novo(Opcoes{
		Baixador:       &baixadorFake{},
		Selecionador:   &selecionadorFake{},
		BaseDir:        t.TempDir(),
		LogRodadasPath: logPath,
		Agora:          func() time.Time { return time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC) },
		GerarID:        func() string { return "x" },
	})
}

func TestRegistrarRodadaOrdenaPorScore(t *testing.T) {
	log := filepath.Join(t.TempDir(), "rodadas.md")
	s := servidorComLog(t, log)
	ped := pipeline.NovoPedido("sermao-1", "https://youtu.be/abc", "01:30:00", "02:10:00", s.agora())
	cands := []validacao.Candidato{
		{Hook: "trecho de score medio", Start: "00:01:00", End: "00:01:35", DurationSeconds: 35, Score: 77},
		{Hook: "trecho de score alto", Start: "00:02:00", End: "00:02:45", DurationSeconds: 45, Score: 90},
		{Hook: "trecho de score baixo", Start: "00:03:00", End: "00:03:34", DurationSeconds: 34, Score: 60,
			RequerRevisaoReforcada: true, MotivoRevisao: "possível problema de fidelidade — revisar"},
	}

	s.registrarRodada(ped, cands)

	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("log não foi escrito: %v", err)
	}
	out := string(b)

	if !strings.Contains(out, "## Rodada 1") {
		t.Errorf("faltou cabeçalho da rodada 1:\n%s", out)
	}
	for _, q := range []string{"sermao-1", "https://youtu.be/abc", "01:30:00 → 02:10:00", "Candidatos: 3"} {
		if !strings.Contains(out, q) {
			t.Errorf("faltou contexto %q no log:\n%s", q, out)
		}
	}
	// Ordenado por score desc: 90 antes de 77 antes de 60.
	i90 := strings.Index(out, "trecho de score alto")
	i77 := strings.Index(out, "trecho de score medio")
	i60 := strings.Index(out, "trecho de score baixo")
	if !(i90 < i77 && i77 < i60) {
		t.Errorf("candidatos não ordenados por score desc: alto=%d medio=%d baixo=%d", i90, i77, i60)
	}
	// A marcação de revisão aparece.
	if !strings.Contains(out, "possível problema de fidelidade") {
		t.Errorf("faltou o motivo de revisão no log:\n%s", out)
	}
}

func TestRegistrarRodadaIncluiTitulo(t *testing.T) {
	log := filepath.Join(t.TempDir(), "rodadas.md")
	s := servidorComLog(t, log)
	ped := pipeline.NovoPedido("s", "u", "00:00:00", "00:10:00", s.agora())
	ped.Titulo = "Culto de Domingo — A graça de Deus"
	s.registrarRodada(ped, []validacao.Candidato{{Hook: "h", Score: 80}})

	out, _ := os.ReadFile(log)
	if !strings.Contains(string(out), "- Título: Culto de Domingo — A graça de Deus") {
		t.Errorf("faltou o título no log:\n%s", out)
	}
}

func TestRegistrarRodadaSemTituloOmiteLinha(t *testing.T) {
	log := filepath.Join(t.TempDir(), "rodadas.md")
	s := servidorComLog(t, log)
	ped := pipeline.NovoPedido("s", "u", "00:00:00", "00:10:00", s.agora()) // sem Titulo
	s.registrarRodada(ped, []validacao.Candidato{{Hook: "h", Score: 80}})

	out, _ := os.ReadFile(log)
	if strings.Contains(string(out), "- Título:") {
		t.Errorf("sem título, a linha não deveria aparecer:\n%s", out)
	}
}

func TestRegistrarRodadaNaoAlteraOrdemOriginal(t *testing.T) {
	s := servidorComLog(t, filepath.Join(t.TempDir(), "r.md"))
	ped := pipeline.NovoPedido("s", "u", "00:00:00", "00:10:00", s.agora())
	cands := []validacao.Candidato{
		{Hook: "primeiro (score baixo)", Score: 50},
		{Hook: "segundo (score alto)", Score: 95},
	}
	s.registrarRodada(ped, cands)
	// A ordenação do log é numa cópia — a fatia original (usada pelos índices de
	// aprovação) não pode ter sido reordenada.
	if cands[0].Hook != "primeiro (score baixo)" || cands[1].Hook != "segundo (score alto)" {
		t.Errorf("registrarRodada reordenou a fatia original: %+v", cands)
	}
}

func TestRegistrarRodadaIncrementaNumero(t *testing.T) {
	log := filepath.Join(t.TempDir(), "rodadas.md")
	s := servidorComLog(t, log)
	ped := pipeline.NovoPedido("s", "u", "00:00:00", "00:10:00", s.agora())
	cand := []validacao.Candidato{{Hook: "h", Score: 80}}

	s.registrarRodada(ped, cand)
	s.registrarRodada(ped, cand)
	s.registrarRodada(ped, cand)

	out, _ := os.ReadFile(log)
	for _, q := range []string{"## Rodada 1", "## Rodada 2", "## Rodada 3"} {
		if !strings.Contains(string(out), q) {
			t.Errorf("faltou %q (numeração não incrementou):\n%s", q, out)
		}
	}
}

// Robustez: se o arquivo foi editado à mão e ficou SEM quebra de linha no fim, a nova
// rodada não pode colar no fim da anterior (o cabeçalho tem que ficar em linha própria).
func TestRegistrarRodadaSeparaMesmoSemQuebraFinal(t *testing.T) {
	log := filepath.Join(t.TempDir(), "rodadas.md")
	// Simula um log editado à mão terminando sem "\n".
	if err := os.WriteFile(log, []byte("## Rodada 1 — x\n\n- Pedido: a\n| ... | crê. |"), 0644); err != nil {
		t.Fatal(err)
	}
	s := servidorComLog(t, log)
	ped := pipeline.NovoPedido("s", "u", "00:00:00", "00:10:00", s.agora())
	s.registrarRodada(ped, []validacao.Candidato{{Hook: "h", Score: 80}})

	out, _ := os.ReadFile(log)
	if strings.Contains(string(out), "crê. |## Rodada") {
		t.Errorf("cabeçalho colou no fim da rodada anterior:\n%s", out)
	}
	if !strings.Contains(string(out), "\n## Rodada 2") {
		t.Errorf("nova rodada deveria começar em linha própria:\n%s", out)
	}
}

// A numeração é contínua mesmo entre "reinícios" (novo Servidor sobre o mesmo arquivo).
func TestRegistrarRodadaContinuaAposReinicio(t *testing.T) {
	log := filepath.Join(t.TempDir(), "rodadas.md")
	ped := pipeline.NovoPedido("s", "u", "00:00:00", "00:10:00", time.Now())
	cand := []validacao.Candidato{{Hook: "h", Score: 80}}

	s1 := servidorComLog(t, log)
	s1.registrarRodada(ped, cand)
	s2 := servidorComLog(t, log) // "reinício": outro Servidor, mesmo arquivo
	s2.registrarRodada(ped, cand)

	out, _ := os.ReadFile(log)
	if !strings.Contains(string(out), "## Rodada 2") {
		t.Errorf("numeração não continuou após reinício:\n%s", out)
	}
}

// Integração: a fase leve, ao concluir, registra uma rodada no log.
func TestFaseLeveRegistraRodada(t *testing.T) {
	log := filepath.Join(t.TempDir(), "rodadas.md")
	sel := &selecionadorFake{cands: []validacao.Candidato{{Hook: "do fluxo", Start: "00:00:10", End: "00:00:40", DurationSeconds: 30, Score: 82}}}
	base := t.TempDir()
	s := Novo(Opcoes{
		Baixador:       &baixadorFake{transc: "x", base: base},
		Selecionador:   sel,
		BaseDir:        base,
		LogRodadasPath: log,
		Agora:          func() time.Time { return time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC) },
		GerarID:        func() string { return "teste-1" },
	})
	criarPedidoOK(t, s)
	esperarStatus(t, s, "teste-1", pipeline.EstadoAguardandoAprovacao)

	out, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("fase leve não registrou rodada: %v", err)
	}
	if !strings.Contains(string(out), "## Rodada 1") || !strings.Contains(string(out), "do fluxo") {
		t.Errorf("rodada da fase leve não ficou completa:\n%s", string(out))
	}
}
