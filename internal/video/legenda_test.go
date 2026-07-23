package video

import (
	"strings"
	"testing"

	"srtclean/internal/harness"
)

// A legenda reutiliza harness.Frasear: dá o texto LIMPO (sem rolagem, sem ">>", sem
// duplicação), então os blocos herdam essa limpeza. Este teste alimenta legendas rolling
// cruas e confirma que os blocos saem limpos e com no máximo 2 linhas.
func TestBlocosLegendaReusaTextoLimpo(t *testing.T) {
	// Legenda "rolling" crua (2 linhas por timestamp, texto acumulando), como o YouTube.
	transc := strings.Join([]string{
		"[00:00:00] a graça de Deus",
		"[00:00:00] a graça de Deus é suficiente para todos nós",
		"[00:00:02] é suficiente para todos nós",
		"[00:00:02] é suficiente para todos nós hoje e sempre.",
	}, "\n")
	frases := harness.Frasear(transc)

	blocos := BlocosLegenda(frases, 0, 60000, 32, 2)
	if len(blocos) == 0 {
		t.Fatal("esperava ao menos um bloco de legenda")
	}
	junto := ""
	for _, b := range blocos {
		junto += " " + b.Texto
		// nunca mais de 2 linhas (spec-12)
		if n := strings.Count(b.Texto, "\n") + 1; n > 2 {
			t.Errorf("bloco com %d linhas (máx 2): %q", n, b.Texto)
		}
	}
	// Desduplicado: "a graça de Deus" aparece uma única vez no texto todo.
	if c := strings.Count(junto, "a graça de Deus"); c != 1 {
		t.Errorf("texto duplicado (rolling não desduplicada): %d ocorrências em %q", c, junto)
	}
	if strings.Contains(junto, ">>") {
		t.Errorf("marcador de locutor vazou para a legenda: %q", junto)
	}
}

func TestBlocosLegendaQuebraEmDuasLinhas(t *testing.T) {
	// Uma frase longa deve quebrar em linhas de <= charsPorLinha e blocos de <= 2 linhas.
	transc := "[00:00:00] Deus nos criou para viver em comunhão com ele e com o próximo todos os dias da vida."
	frases := harness.Frasear(transc)
	blocos := BlocosLegenda(frases, 0, 60000, 20, 2)
	if len(blocos) < 2 {
		t.Fatalf("frase longa deveria virar vários blocos, veio %d", len(blocos))
	}
	for _, b := range blocos {
		for _, linha := range strings.Split(b.Texto, "\n") {
			if len([]rune(linha)) > 20+12 { // folga: só quebra em limite de palavra
				t.Errorf("linha larga demais: %q", linha)
			}
		}
	}
}

func TestBlocosLegendaRebaseiaTempo(t *testing.T) {
	// Frase em tempo absoluto; o bloco deve sair rebaseado ao início do trecho (startMs).
	transc := "[01:30:10] O amor de Cristo alcança o pecador."
	frases := harness.Frasear(transc)
	// Trecho começa em 01:30:00 = 5400000ms; a frase começa em 01:30:10 (10s depois).
	start := 5400000
	blocos := BlocosLegenda(frases, start, start+60000, 32, 2)
	if len(blocos) == 0 {
		t.Fatal("esperava um bloco")
	}
	// A frase está 10s após o início do trecho → bloco começa ~10000ms (rebaseado).
	if blocos[0].InicioMs < 9000 || blocos[0].InicioMs > 11000 {
		t.Errorf("tempo do bloco não rebaseado ao início do trecho: %dms", blocos[0].InicioMs)
	}
}

func TestBlocosLegendaForaDoTrecho(t *testing.T) {
	transc := "[00:00:05] Isto está fora da janela do trecho."
	frases := harness.Frasear(transc)
	if b := BlocosLegenda(frases, 100000, 160000, 32, 2); len(b) != 0 {
		t.Errorf("frase fora de [start,end] não deveria virar bloco: %+v", b)
	}
}
