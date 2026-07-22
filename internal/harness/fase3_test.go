package harness

import (
	"strings"
	"testing"

	"srtclean/internal/validacao"
)

// frasesDe monta uma lista de frases de duração fixa (segundos) para os testes puros.
// Cada frase i ocupa [i*dur, (i+1)*dur) em ms.
func frasesFixas(n, durSeg int) []Frase {
	var fs []Frase
	for i := 0; i < n; i++ {
		fs = append(fs, Frase{
			InicioMs: i * durSeg * 1000,
			FimMs:    (i + 1) * durSeg * 1000,
			Texto:    "frase " + string(rune('a'+i)) + ".",
			Completa: true,
			FimLimpo: true,
		})
	}
	return fs
}

func TestDelimitarCresceParaFrente(t *testing.T) {
	fs := frasesFixas(6, 10) // 6 frases de 10s, [0..60000]
	// Âncora na primeira; bloco amplo. Deve crescer para frente até 30s.
	lo, hi, ok, motivo := Delimitar(fs, 0, 0, 100000)
	if !ok {
		t.Fatalf("esperava viável, veio inviável: %s", motivo)
	}
	if lo != 0 || fs[lo].InicioMs != 0 || fs[hi].FimMs != 30000 {
		t.Errorf("esperava [0,30000] começando na âncora, veio lo=%d [%d,%d]", lo, fs[lo].InicioMs, fs[hi].FimMs)
	}
}

func TestDelimitarCresceParaTras(t *testing.T) {
	fs := frasesFixas(6, 10) // [0..60000]
	// Âncora na frase 3 ([30000,40000]); bloco [0,40000] impede crescer para frente
	// (a frase 4 termina em 50000 > 40000). Deve crescer para TRÁS até 30s.
	lo, hi, ok, motivo := Delimitar(fs, 3, 0, 40000)
	if !ok {
		t.Fatalf("esperava viável, veio inviável: %s", motivo)
	}
	if fs[hi].FimMs != 40000 {
		t.Errorf("fim deveria ficar na borda do bloco (40000), veio %d", fs[hi].FimMs)
	}
	if fs[lo].InicioMs != 10000 { // recuou de 30000 -> 10000 para somar 30s
		t.Errorf("esperava início 10000 (cresceu para trás), veio %d", fs[lo].InicioMs)
	}
}

func TestDelimitarNaoUltrapassaBloco(t *testing.T) {
	fs := frasesFixas(6, 10) // [0..60000]
	// Bloco pequeno [20000,40000] = só 20s de conteúdo. Âncora dentro (frase 2).
	// Não há como formar 30s sem invadir o bloco vizinho -> descarte.
	_, _, ok, motivo := Delimitar(fs, 2, 20000, 40000)
	if ok {
		t.Fatalf("esperava inviável (bloco pequeno), veio viável")
	}
	if !strings.Contains(motivo, "30") {
		t.Errorf("motivo deveria citar não formar 30s: %q", motivo)
	}
}

func TestDelimitarAncoraLongaDemais(t *testing.T) {
	fs := []Frase{{InicioMs: 0, FimMs: 61000, Texto: "frase muito longa.", Completa: true}}
	_, _, ok, motivo := Delimitar(fs, 0, 0, 100000)
	if ok {
		t.Fatalf("esperava inviável (âncora > 58s)")
	}
	if !strings.Contains(motivo, "58") {
		t.Errorf("motivo deveria citar 58s: %q", motivo)
	}
}

func TestDelimitarRespeita58s(t *testing.T) {
	// f0=25s, f1=35s. Somar as duas = 60s > 58s -> não pode; f0 sozinha=25s < 30s.
	// Logo é inviável (a única forma de crescer estouraria 58s).
	fs := []Frase{
		{InicioMs: 0, FimMs: 25000, Texto: "frase a.", Completa: true},
		{InicioMs: 25000, FimMs: 60000, Texto: "frase b.", Completa: true},
	}
	_, _, ok, _ := Delimitar(fs, 0, 0, 100000)
	if ok {
		t.Fatalf("esperava inviável: crescer estouraria 58s e sozinha não chega a 30s")
	}
}

func TestDelimitarLandsAcimaDe30(t *testing.T) {
	// f0=10s, f1=35s -> somar dá 45s (<=58), viável, início 0 fim 45000.
	fs := []Frase{
		{InicioMs: 0, FimMs: 10000, Texto: "frase a.", Completa: true},
		{InicioMs: 10000, FimMs: 45000, Texto: "frase b.", Completa: true},
	}
	lo, hi, ok, _ := Delimitar(fs, 0, 0, 100000)
	if !ok || fs[lo].InicioMs != 0 || fs[hi].FimMs != 45000 {
		t.Errorf("esperava [0,45000] viável, veio [%d,%d] ok=%v", fs[lo].InicioMs, fs[hi].FimMs, ok)
	}
}

func TestDelimitarTerminaEmFraseCompleta(t *testing.T) {
	// A última frase (índice 3) é um fragmento incompleto (sem pontuação). O trecho
	// não pode terminar nela: deve recuar para a frase completa anterior.
	fs := []Frase{
		{InicioMs: 0, FimMs: 12000, Texto: "frase a.", Completa: true},
		{InicioMs: 12000, FimMs: 24000, Texto: "frase b.", Completa: true},
		{InicioMs: 24000, FimMs: 36000, Texto: "frase c.", Completa: true},
		{InicioMs: 36000, FimMs: 40000, Texto: "fragmento sem ponto", Completa: false},
	}
	_, hi, ok, _ := Delimitar(fs, 0, 0, 100000)
	if !ok {
		t.Fatalf("esperava viável")
	}
	if !fs[hi].Completa {
		t.Errorf("o trecho terminou numa frase incompleta: %q", fs[hi].Texto)
	}
	if fs[hi].FimMs != 36000 {
		t.Errorf("esperava terminar na frase c (36000), veio %d", fs[hi].FimMs)
	}
}

func TestFrasearLimpo(t *testing.T) {
	tr := "[00:00:00] Primeira frase completa.\n[00:00:10] Segunda frase aqui.\n[00:00:20] Terceira frase final."
	fs := Frasear(tr)
	if len(fs) != 3 {
		t.Fatalf("esperava 3 frases, veio %d: %+v", len(fs), fs)
	}
	if fs[0].InicioMs != 0 || fs[1].InicioMs != 10000 || fs[2].InicioMs != 20000 {
		t.Errorf("tempos de início inesperados: %+v", fs)
	}
	// FimMs = tempo da última palavra da frase (cada frase aqui está numa linha só,
	// então FimMs == InicioMs). Todas terminam em ponto → Completa.
	if fs[0].FimMs != 0 || fs[1].FimMs != 10000 || fs[2].FimMs != 20000 {
		t.Errorf("FimMs (última palavra) inesperado: %+v", fs)
	}
	for _, f := range fs {
		if !f.Completa {
			t.Errorf("frase deveria ser completa: %q", f.Texto)
		}
	}
}

func TestFrasearDeduplicaRolling(t *testing.T) {
	// Legendas "rolling": linhas que repetem o texto que continua na tela.
	tr := strings.Join([]string{
		"[00:00:00] que Cristo pode curar um paralítico.",
		"[00:00:00] que Cristo pode curar um paralítico. Isso é pouco ainda.",
		"[00:00:04] Isso é pouco ainda.",
		"[00:00:04] Isso é pouco ainda. João registra tudo.",
	}, "\n")
	fs := Frasear(tr)
	if len(fs) != 3 {
		t.Fatalf("esperava 3 frases após dedup, veio %d: %+v", len(fs), fs)
	}
	// A frase repetida ("Isso é pouco ainda.") deve aparecer UMA vez.
	n := 0
	for _, f := range fs {
		if strings.Contains(f.Texto, "Isso é pouco ainda") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("dedup falhou: 'Isso é pouco ainda' aparece %d vezes", n)
	}
	if !strings.Contains(fs[2].Texto, "João registra tudo") {
		t.Errorf("última frase inesperada: %q", fs[2].Texto)
	}
	// "João registra tudo" só aparece a partir de 00:00:04.
	if fs[2].InicioMs != 4000 {
		t.Errorf("início da 3a frase = %d, queria 4000", fs[2].InicioMs)
	}
}

func TestAcharAncora(t *testing.T) {
	fs := Frasear("[00:00:00] A graça de Deus é suficiente.\n[00:00:10] O Senhor é o pastor.")
	if i, ok := AcharAncora(fs, "a graça de Deus é suficiente"); !ok || i != 0 {
		t.Errorf("AcharAncora = %d,%v; queria 0,true", i, ok)
	}
	if _, ok := AcharAncora(fs, "isto não existe na fala"); ok {
		t.Error("esperava não encontrar âncora inexistente")
	}
}

// Regressão: o modelo devolve a frase-âncora com MAIS DE UMA frase (ponto no meio),
// mas o Frasear quebra a transcrição em sentenças. Casar pela âncora inteira nunca
// bateria; casar pelas primeiras palavras da PRIMEIRA frase da âncora encontra. Ambas
// as âncoras abaixo são trechos REAIS desta transcrição (legendas "rolling" do YouTube).

func TestAcharAncoraMultiFraseResgate(t *testing.T) {
	// Trecho real ~01:36:46 — a divindade/resgate: "Foi Jesus quem o procurou."
	tr := strings.Join([]string{
		"[01:36:46] ele nem sequer procura Jesus,",
		"[01:36:46] ele nem sequer procura Jesus, melhor dizendo e acrescentando, ele nem",
		"[01:36:48] melhor dizendo e acrescentando, ele nem",
		"[01:36:48] melhor dizendo e acrescentando, ele nem conhecia. Foi Jesus quem o procurou. Foi",
		"[01:36:51] conhecia. Foi Jesus quem o procurou. Foi",
		"[01:36:51] conhecia. Foi Jesus quem o procurou. Foi Jesus quem entrou naquela caverna para",
		"[01:36:55] Jesus quem entrou naquela caverna para",
		"[01:36:55] Jesus quem entrou naquela caverna para retirar o homem de lá. E esse milagre",
	}, "\n")
	fs := Frasear(tr)
	// Âncora multi-frase, como o modelo às vezes devolve.
	ancora := "Foi Jesus quem o procurou. Foi Jesus quem entrou naquela caverna para retirar o homem de lá."
	i, ok := AcharAncora(fs, ancora)
	if !ok {
		t.Fatalf("âncora multi-frase do resgate deveria ser encontrada; não foi")
	}
	if !strings.HasPrefix(fs[i].Texto, "Foi Jesus quem o procurou") {
		t.Errorf("frase casada = %q; queria começar em \"Foi Jesus quem o procurou\"", fs[i].Texto)
	}
}

func TestAcharAncoraMultiFraseDivindade(t *testing.T) {
	// Trecho real ~01:52:57 — a divindade de Cristo: "Jesus é Deus."
	tr := strings.Join([]string{
		"[01:52:57] mas os fariseus entenderam. E creio no",
		"[01:52:57] mas os fariseus entenderam. E creio no Senhor Jesus que vocês também. Jesus é",
		"[01:52:59] Senhor Jesus que vocês também. Jesus é",
		"[01:53:00] Senhor Jesus que vocês também. Jesus é Deus.",
		"[01:53:02] Deus.",
		"[01:53:02] Deus. Eles têm toda a prerrogativa de Deus.",
		"[01:53:04] Eles têm toda a prerrogativa de Deus.",
		"[01:53:04] Eles têm toda a prerrogativa de Deus. Ele é o próprio Deus vivo. E João vem",
	}, "\n")
	fs := Frasear(tr)
	// Âncora multi-frase, primeira sentença bem curta ("Jesus é Deus.").
	ancora := "Jesus é Deus. Eles têm toda a prerrogativa de Deus."
	i, ok := AcharAncora(fs, ancora)
	if !ok {
		t.Fatalf("âncora multi-frase da divindade deveria ser encontrada; não foi")
	}
	if !strings.Contains(fs[i].Texto, "Jesus é Deus") {
		t.Errorf("frase casada = %q; queria conter \"Jesus é Deus\"", fs[i].Texto)
	}
}

func TestPrimeiraFrase(t *testing.T) {
	casos := map[string]string{
		"Jesus é Deus. Eles têm toda a prerrogativa de Deus.": "Jesus é Deus.",
		"Foi Jesus quem o procurou. Foi Jesus quem entrou.":   "Foi Jesus quem o procurou.",
		"uma frase sem ponto final":                           "uma frase sem ponto final",
	}
	for in, want := range casos {
		if got := primeiraFrase(in); got != want {
			t.Errorf("primeiraFrase(%q) = %q; queria %q", in, got, want)
		}
	}
}

func TestFase3DelimitarPontaAPonta(t *testing.T) {
	tr := strings.Join([]string{
		"[00:00:00] A graça de Deus é suficiente para você.",
		"[00:00:10] Ele sustenta o fraco todos os dias.",
		"[00:00:20] Por isso não temas o amanhã.",
		"[00:00:30] O Senhor é o teu pastor fiel.",
		"[00:00:40] Nada te faltará na jornada.",
		"[00:00:50] Descansa nas mãos do Pai.",
	}, "\n")
	mapa := Mapa{
		TemaCentral: "graça",
		Blocos: []BlocoEnsino{
			{Assunto: "graça", InicioAprox: "00:00:00", FimAprox: "00:01:00"},
		},
	}
	cands := []CandidatoBruto{
		{Bloco: "graça", FraseAncora: "A graça de Deus é suficiente"},
		{Bloco: "graça", FraseAncora: "isto não existe na transcrição"},
	}

	oks, descs := Fase3Delimitar(cands, mapa, tr)
	if len(oks) != 1 {
		t.Fatalf("esperava 1 candidato delimitado, veio %d", len(oks))
	}
	if len(descs) != 1 {
		t.Fatalf("esperava 1 descarte, veio %d", len(descs))
	}
	c := oks[0]
	if c.Start != "00:00:00.000" || c.End != "00:00:30.000" {
		t.Errorf("tempos inesperados: start=%q end=%q", c.Start, c.End)
	}
	if c.DuracaoSegundos < 30 || c.DuracaoSegundos > 58 {
		t.Errorf("duração fora de 30-58s: %v", c.DuracaoSegundos)
	}
	// hook = a frase completa a partir do start (não só a frase-âncora).
	if c.Hook != "A graça de Deus é suficiente para você." {
		t.Errorf("hook inesperado: %q", c.Hook)
	}
}

// --- Testes com TRECHO REAL desta transcrição (regressão dos dois bugs achados) ---

// trechoReal é um recorte fiel da transcrição do sermão mg83gcM4ctw (ilustração da
// caverna → transição para João 5), com as legendas "rolling" como vêm do YouTube.
const trechoReal = `[01:35:45] mergulhadores arriscaram a própria vida
[01:35:45] mergulhadores arriscaram a própria vida para retirar um a um daqueles
[01:35:48] para retirar um a um daqueles
[01:35:48] para retirar um a um daqueles adolescentes da caverna e o último, o
[01:35:51] adolescentes da caverna e o último, o
[01:35:51] adolescentes da caverna e o último, o treinador. Um dos mergulhadores,
[01:35:53] treinador. Um dos mergulhadores,
[01:35:53] treinador. Um dos mergulhadores, Samancunã, morreu durante a operação ao
[01:35:56] Samancunã, morreu durante a operação ao
[01:35:56] Samancunã, morreu durante a operação ao levar cilindros de oxigênio para a
[01:35:58] levar cilindros de oxigênio para a
[01:35:58] levar cilindros de oxigênio para a equipe de resgate. Aqueles jovens, eles
[01:36:01] equipe de resgate. Aqueles jovens, eles
[01:36:01] equipe de resgate. Aqueles jovens, eles não foram salvos porque eles encontraram
[01:36:04] não foram salvos porque eles encontraram
[01:36:04] não foram salvos porque eles encontraram o caminho de volta pra caverna para sair
[01:36:07] o caminho de volta pra caverna para sair
[01:36:07] o caminho de volta pra caverna para sair daquele lugar. Eles foram salvos porque
[01:36:10] daquele lugar. Eles foram salvos porque
[01:36:10] daquele lugar. Eles foram salvos porque em uma mega operação alguém foi até a
[01:36:14] em uma mega operação alguém foi até a
[01:36:14] em uma mega operação alguém foi até a caverna e retirou-os pelo único caminho
[01:36:16] caverna e retirou-os pelo único caminho
[01:36:17] caverna e retirou-os pelo único caminho possível.
[01:36:18] possível.
[01:36:18] possível. Olhe pro texto. É exatamente nesse ponto
[01:36:21] Olhe pro texto. É exatamente nesse ponto
[01:36:21] Olhe pro texto. É exatamente nesse ponto que João 5 nos conduz. Aqui à margem do
[01:36:24] que João 5 nos conduz. Aqui à margem do
[01:36:24] que João 5 nos conduz. Aqui à margem do tanque de Betesda, nós encontramos uma
[01:36:26] tanque de Betesda, nós encontramos uma
[01:36:26] tanque de Betesda, nós encontramos uma multidão enferma. Entre eles, um homem
[01:36:29] multidão enferma. Entre eles, um homem
[01:36:29] multidão enferma. Entre eles, um homem paralítico, havia 38 anos, ele também`

// Bug 1 (regressão): quando o trecho cresce para trás, o hook TEM que ser reescrito
// como a frase de abertura real do start — nunca ficar sendo a frase-âncora que caiu
// no meio do trecho. Aqui a âncora "Eles foram salvos" força crescer para trás
// (o bloco termina logo após ela), então o hook deve mudar e bater com o start.
func TestFase3RealHookBateComStart(t *testing.T) {
	mapa := Mapa{Blocos: []BlocoEnsino{
		// Bloco que termina logo após a frase-âncora ("...possível." em 01:36:17):
		// impede crescer para frente, forçando crescer para trás.
		{Assunto: "resgate", InicioAprox: "01:35:40", FimAprox: "01:36:17"},
	}}
	cands := []CandidatoBruto{{Bloco: "resgate", FraseAncora: "Eles foram salvos porque"}}

	oks, descs := Fase3Delimitar(cands, mapa, trechoReal)
	if len(oks) != 1 {
		t.Fatalf("esperava 1 delimitado, veio %d (descartes: %+v)", len(oks), descs)
	}
	c := oks[0]

	// O hook não pode continuar sendo a frase-âncora do meio do trecho.
	if strings.Contains(c.Hook, "Eles foram salvos") {
		t.Errorf("bug 1: hook continuou sendo a frase-âncora do meio: %q", c.Hook)
	}
	// hook TEM que bater com o start: a frase encontrada no start é o próprio hook.
	frases := Frasear(trechoReal)
	startMs, _ := validacao.HmsToMs(c.Start)
	achouNoStart := false
	for _, f := range frases {
		if f.InicioMs == startMs {
			if f.Texto != c.Hook {
				t.Errorf("bug 1: hook (%q) não é a frase do start (%q)", c.Hook, f.Texto)
			}
			achouNoStart = true
		}
	}
	if !achouNoStart {
		t.Errorf("bug 1: nenhuma frase começa exatamente no start %q", c.Start)
	}
}

// Bug 2 (regressão): o end tem que cair em FIM de frase completa — nunca no começo
// da frase seguinte (o caso real cortava em "...Aqui à margem do..."). Aqui o trecho
// deve terminar em "...possível." e o end NÃO pode ser 01:36:21 (início de "Aqui").
func TestFase3RealEndCaiEmFimDeFrase(t *testing.T) {
	mapa := Mapa{Blocos: []BlocoEnsino{
		{Assunto: "resgate", InicioAprox: "01:35:40", FimAprox: "01:36:17"},
	}}
	cands := []CandidatoBruto{{Bloco: "resgate", FraseAncora: "Eles foram salvos porque"}}

	oks, _ := Fase3Delimitar(cands, mapa, trechoReal)
	if len(oks) != 1 {
		t.Fatalf("esperava 1 delimitado")
	}
	c := oks[0]

	// O end deve casar com o fim de uma frase COMPLETA da transcrição.
	frases := Frasear(trechoReal)
	endMs, _ := validacao.HmsToMs(c.End)
	var fraseDoEnd *Frase
	for i := range frases {
		if frases[i].FimMs == endMs {
			fraseDoEnd = &frases[i]
		}
	}
	if fraseDoEnd == nil || !fraseDoEnd.Completa {
		t.Fatalf("bug 2: end %q não cai no fim de uma frase completa", c.End)
	}
	if !terminaFrase(strings.Fields(fraseDoEnd.Texto)[len(strings.Fields(fraseDoEnd.Texto))-1]) {
		t.Errorf("bug 2: frase do end não termina em pontuação: %q", fraseDoEnd.Texto)
	}
	// Especificamente, não pode cortar no começo de "Aqui à margem do".
	if c.End == "01:36:21.000" {
		t.Errorf("bug 2: end caiu no início da frase seguinte (Aqui à margem do)")
	}
	if !strings.Contains(fraseDoEnd.Texto, "possível") {
		t.Errorf("esperava terminar na frase que fecha em 'possível.', veio %q", fraseDoEnd.Texto)
	}
}

// Forward-first no trecho real: quando um fim limpo à frente já forma 30 s a partir
// da âncora, o trecho cresce para frente e o start PERMANECE na âncora (não recua).
// A âncora "mergulhadores arriscaram" (01:35:45) alcança o fim limpo "...possível."
// (01:36:17) em 32 s sem precisar recuar.
func TestFase3RealPrefereCrescerParaFrente(t *testing.T) {
	mapa := Mapa{Blocos: []BlocoEnsino{
		{Assunto: "resgate", InicioAprox: "01:35:40", FimAprox: "01:37:00"}, // bloco amplo à frente
	}}
	cands := []CandidatoBruto{{Bloco: "resgate", FraseAncora: "mergulhadores arriscaram a própria vida"}}

	oks, descs := Fase3Delimitar(cands, mapa, trechoReal)
	if len(oks) != 1 {
		t.Fatalf("esperava 1 delimitado, veio %d (descartes %+v)", len(oks), descs)
	}
	c := oks[0]
	// start deve permanecer na âncora (cresceu para frente, não para trás).
	if c.Start != "01:35:45.000" {
		t.Errorf("esperava start na âncora (01:35:45), veio %q — cresceu para trás sem precisar", c.Start)
	}
	if !strings.HasPrefix(c.Hook, "mergulhadores arriscaram") {
		t.Errorf("hook deveria ser a frase da âncora: %q", c.Hook)
	}
	// Fim limpo em "...possível." (01:36:17), não em "conduz."/"Aqui" (mesmo bloco).
	if c.End != "01:36:17.000" {
		t.Errorf("esperava fim limpo em 01:36:17 (possível.), veio %q", c.End)
	}
	if c.DuracaoSegundos < 30 || c.DuracaoSegundos > 58 {
		t.Errorf("duração fora de 30-58s: %v", c.DuracaoSegundos)
	}
}
