package main

import (
	"strings"
	"testing"
)

// srtBase cobre, num único SRT, os casos do contrato da spec-01:
// numeração de sequência, setas "-->", tags <i>/{...}, anotações [Aplausos],
// bloco vazio (só anotação), repetição legítima e sobreposição de tempo.
const srtBase = `1
00:00:01,000 --> 00:00:03,000
<i>Bom dia</i> a todos

2
00:00:04,000 --> 00:00:06,000
[Aplausos]

3
00:00:05,000 --> 00:00:08,000
{\an8}de verdade, de verdade eu digo

4
00:00:04,000 --> 00:00:09,000
o Senhor reina
`

func TestCleanDescartaNumeracaoESetas(t *testing.T) {
	lines, blocks := clean(srtBase, -1)

	if blocks != 4 {
		t.Fatalf("esperava 4 blocos lidos, veio %d", blocks)
	}
	// O bloco 2 é só anotação ([Aplausos]) → vira vazio e é ignorado.
	if len(lines) != 3 {
		t.Fatalf("esperava 3 linhas, veio %d: %q", len(lines), lines)
	}
	for _, l := range lines {
		if strings.Contains(l, "-->") {
			t.Errorf("linha ainda contém seta de tempo: %q", l)
		}
		// Número de sequência solto não deve virar linha de saída.
		if strings.TrimSpace(l) == "1" || strings.TrimSpace(l) == "4" {
			t.Errorf("número de sequência vazou como linha: %q", l)
		}
	}
}

func TestCleanRemoveMarcacao(t *testing.T) {
	lines, _ := clean(srtBase, -1)
	joined := strings.Join(lines, "\n")

	for _, marca := range []string{"<i>", "</i>", "{\\an8}", "[Aplausos]"} {
		if strings.Contains(joined, marca) {
			t.Errorf("marcação %q não foi removida: %q", marca, joined)
		}
	}
	// A primeira fala mantém as palavras, sem a tag.
	if lines[0] != "[00:00:01] Bom dia a todos" {
		t.Errorf("linha 0 inesperada: %q", lines[0])
	}
}

func TestCleanPreservaRepeticaoLegitima(t *testing.T) {
	lines, _ := clean(srtBase, -1)
	if !strings.Contains(lines[1], "de verdade, de verdade eu digo") {
		t.Errorf("repetição legítima foi perdida: %q", lines[1])
	}
}

func TestCleanIgnoraBlocoVazio(t *testing.T) {
	lines, _ := clean(srtBase, -1)
	for _, l := range lines {
		if strings.Contains(l, "Aplausos") {
			t.Errorf("bloco de anotação não deveria gerar linha: %q", l)
		}
	}
}

func TestCleanIniciosNaoRetrocedem(t *testing.T) {
	// O bloco 4 começa em 00:00:04, antes do bloco 3 (00:00:05). A saída deve
	// forçar o início a não retroceder (sobreposição de tempo do autocaption).
	lines, _ := clean(srtBase, -1)
	last := -1
	for _, l := range lines {
		hms := l[1:9] // "[HH:MM:SS] ..." -> "HH:MM:SS"
		ms, ok := hmsToMs(hms)
		if !ok {
			t.Fatalf("não parseei o tempo da linha %q", l)
		}
		if ms < last {
			t.Errorf("início retrocedeu: %q (ms=%d) < %d", l, ms, last)
		}
		last = ms
	}
}

func TestCleanUntilCorta(t *testing.T) {
	untilMs, _ := hmsToMs("00:00:05")
	lines, _ := clean(srtBase, untilMs)

	// Só o bloco 1 (00:00:01) fica; a partir de 00:00:05 corta.
	if len(lines) != 1 {
		t.Fatalf("com -until 00:00:05 esperava 1 linha, veio %d: %q", len(lines), lines)
	}
	if lines[0] != "[00:00:01] Bom dia a todos" {
		t.Errorf("linha inesperada com -until: %q", lines[0])
	}
}

func TestCleanTextNaoTocaPalavras(t *testing.T) {
	in := "  <i>Palavra</i>   {\\an8}viva  [Música] mesmo  "
	got := cleanText(in)
	want := "Palavra viva mesmo"
	if got != want {
		t.Errorf("cleanText = %q, queria %q", got, want)
	}
}

func TestHmsToMs(t *testing.T) {
	cases := map[string]int{
		"00:00:00":     0,
		"00:00:01,000": 1000,
		"00:01:02.500": 62500,
		"01:00:00":     3600000,
	}
	for in, want := range cases {
		got, ok := hmsToMs(in)
		if !ok || got != want {
			t.Errorf("hmsToMs(%q) = %d,%v; queria %d", in, got, ok, want)
		}
	}
	if _, ok := hmsToMs("nao-e-tempo"); ok {
		t.Errorf("hmsToMs devia falhar em entrada inválida")
	}
}

func TestFormatMs(t *testing.T) {
	if got := formatMs(62500); got != "00:01:02" {
		t.Errorf("formatMs(62500) = %q, queria 00:01:02", got)
	}
}
