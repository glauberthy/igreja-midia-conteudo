package harness

import (
	"fmt"
	"regexp"
	"strings"

	"srtclean/internal/validacao"
)

// Fase 3 — Delimitação de tempo. 100% CÓDIGO, nenhum timestamp vem do modelo.
//
// A partir da frase-âncora (Fase 2), o código ancora o trecho, cresce por frases
// COMPLETAS dentro das bordas do bloco do mapa até a duração cair em 30–58 s, e
// descarta o que não formar 30 s coerentes. start/end sempre em limites de frase.

const (
	duracaoMinMs = 30000 // 30 s
	duracaoMaxMs = 58000 // 58 s (limite absoluto do Short é 60)
)

// Frase é uma sentença da transcrição. InicioMs é o tempo da primeira palavra;
// FimMs é o tempo da ÚLTIMA palavra (onde o ponto final aparece) — não o início da
// frase seguinte, para o `end` cair sempre no fim da frase, e não já exibindo o
// começo da próxima. Completa indica se a frase termina em pontuação final (. ! ?).
// FimLimpo indica que o fim da frase coincide com uma TROCA de bloco de legenda (a
// frase seguinte só aparece num bloco posterior); encerrar num fim limpo evita cortar
// já exibindo o começo da próxima frase (limitação das legendas "rolling" do YouTube,
// em que o fim de uma frase e o início da próxima às vezes caem no mesmo bloco).
type Frase struct {
	InicioMs int
	FimMs    int
	Texto    string
	Completa bool
	FimLimpo bool
}

// CandidatoDelimitado é o candidato após a Fase 3: com tempo já delimitado por código.
// Ainda SEM avaliação (score/critérios são da Fase 4). Texto é o trecho recortado
// (frases do start ao end), que alimenta a avaliação da Fase 4.
type CandidatoDelimitado struct {
	Bloco           string  `json:"bloco"`
	Hook            string  `json:"hook"`
	Start           string  `json:"start"`
	End             string  `json:"end"`
	DuracaoSegundos float64 `json:"duration_seconds"`
	Texto           string  `json:"texto"`
}

// Descarte registra por que um candidato bruto não virou trecho viável.
type Descarte struct {
	Bloco  string `json:"bloco"`
	Ancora string `json:"frase_ancora"`
	Motivo string `json:"motivo"`
}

var reLinhaTransc = regexp.MustCompile(`^\[(\d{2}:\d{2}:\d{2})\]\s*(.*)$`)

// Fase3Delimitar aplica a delimitação de tempo a cada candidato bruto da Fase 2.
// Devolve os viáveis (com tempo) e os descartados (com motivo).
func Fase3Delimitar(cands []CandidatoBruto, mapa Mapa, transcricao string) ([]CandidatoDelimitado, []Descarte) {
	frases := Frasear(transcricao)
	var oks []CandidatoDelimitado
	var descs []Descarte

	for _, c := range cands {
		anchorIdx, achou := AcharAncora(frases, c.FraseAncora)
		if !achou {
			descs = append(descs, Descarte{c.Bloco, c.FraseAncora, "frase-âncora não encontrada na transcrição"})
			continue
		}
		// Bloco resolvido pelo TEMPO da âncora (mais confiável que o texto do modelo).
		iniBloco, fimBloco := bordasDoBloco(mapa, frases[anchorIdx])

		lo, hi, ok, motivo := Delimitar(frases, anchorIdx, iniBloco, fimBloco)
		if !ok {
			descs = append(descs, Descarte{c.Bloco, c.FraseAncora, motivo})
			continue
		}
		inicioMs := frases[lo].InicioMs
		fimMs := frases[hi].FimMs
		var partes []string
		for k := lo; k <= hi; k++ {
			partes = append(partes, frases[k].Texto)
		}
		oks = append(oks, CandidatoDelimitado{
			Bloco: c.Bloco,
			// hook = a PRIMEIRA frase real a partir do start final. Hook e start
			// têm que bater sempre; se o trecho cresceu para trás, o hook deixa de
			// ser a frase-âncora e passa a ser a frase de abertura de fato.
			Hook:            frases[lo].Texto,
			Start:           validacao.MsParaHms(inicioMs) + ".000",
			End:             validacao.MsParaHms(fimMs) + ".000",
			DuracaoSegundos: float64(fimMs-inicioMs) / 1000.0,
			Texto:           strings.Join(partes, " "),
		})
	}
	return oks, descs
}

// Delimitar (LÓGICA PURA) cresce o trecho a partir da frase-âncora (anchorIdx),
// dentro das bordas [blocoIniMs, blocoFimMs], até a duração cair em 30–58 s. Devolve
// os índices de frase [lo, hi] do trecho. Estratégia: cresce PRIMEIRO para frente
// (mantendo a âncora como abertura); só cresce para TRÁS se não houver material
// suficiente à frente dentro do bloco. O trecho sempre TERMINA numa frase completa.
// ok=false com motivo se não formar 30 s coerentes.
func Delimitar(frases []Frase, anchorIdx, blocoIniMs, blocoFimMs int) (lo, hi int, ok bool, motivo string) {
	if anchorIdx < 0 || anchorIdx >= len(frases) {
		return 0, 0, false, "âncora fora da transcrição"
	}
	// As bordas nunca podem excluir a própria âncora (bordas do mapa são aproximadas).
	if blocoIniMs > frases[anchorIdx].InicioMs {
		blocoIniMs = frases[anchorIdx].InicioMs
	}
	if blocoFimMs < frases[anchorIdx].FimMs {
		blocoFimMs = frases[anchorIdx].FimMs
	}

	if frases[anchorIdx].FimMs-frases[anchorIdx].InicioMs > duracaoMaxMs {
		return 0, 0, false, fmt.Sprintf("frase-âncora sozinha já passa de %ds", duracaoMaxMs/1000)
	}

	// Preferir encerrar num FIM LIMPO (fim de frase que coincide com troca de bloco de
	// legenda). Se não houver fim limpo viável dentro do bloco, cai para qualquer frase
	// COMPLETA. Em ambos, prefere crescer para frente (âncora como abertura); só cresce
	// para trás quando o material à frente não basta para 30 s.
	limpo := func(f Frase) bool { return f.Completa && f.FimLimpo }
	completa := func(f Frase) bool { return f.Completa }

	if lo, hi, ok = buscarTrecho(frases, anchorIdx, blocoIniMs, blocoFimMs, limpo); ok {
		return lo, hi, true, ""
	}
	if lo, hi, ok = buscarTrecho(frases, anchorIdx, blocoIniMs, blocoFimMs, completa); ok {
		return lo, hi, true, ""
	}
	return 0, 0, false, "não forma 30–58 s terminando em fim de frase dentro do bloco"
}

// buscarTrecho encontra [lo, hi] com a âncora incluída, terminando numa frase aceita
// por `aceitaFim`, com duração em 30–58 s e dentro do bloco. Prefere terminar o mais
// cedo possível à frente (âncora como abertura, lo = âncora); só recua o início (lo)
// quando o material à frente não alcança 30 s.
func buscarTrecho(frases []Frase, anchorIdx, blocoIniMs, blocoFimMs int, aceitaFim func(Frase) bool) (lo, hi int, ok bool) {
	// Passo 1: forward-only (lo = âncora) — o preferido.
	for j := anchorIdx; j < len(frases) && frases[j].FimMs <= blocoFimMs; j++ {
		if !aceitaFim(frases[j]) {
			continue
		}
		dur := frases[j].FimMs - frases[anchorIdx].InicioMs
		if dur > duracaoMaxMs {
			break
		}
		if dur >= duracaoMinMs {
			return anchorIdx, j, true
		}
	}
	// Passo 2: permitir recuar o início até alcançar 30 s (fim ainda aceito).
	for j := anchorIdx; j < len(frases) && frases[j].FimMs <= blocoFimMs; j++ {
		if !aceitaFim(frases[j]) {
			continue
		}
		if frases[j].FimMs-frases[anchorIdx].InicioMs > duracaoMaxMs {
			break // mesmo forward-only já passa de 58 s; recuar só piora
		}
		for k := anchorIdx; k >= 0 && frases[k].InicioMs >= blocoIniMs; k-- {
			dur := frases[j].FimMs - frases[k].InicioMs
			if dur > duracaoMaxMs {
				break
			}
			if dur >= duracaoMinMs {
				return k, j, true
			}
		}
	}
	return 0, 0, false
}

// AcharAncora encontra a frase cujo texto contém a frase-âncora. Casa pelas PRIMEIRAS
// palavras normalizadas da PRIMEIRA sentença da âncora — nunca pela âncora inteira.
// Motivo: o modelo às vezes devolve a âncora com mais de uma frase (ponto no meio) e o
// Frasear quebra a transcrição em sentenças; casar pela âncora inteira faria a chave
// atravessar uma fronteira de frase e nunca bater com nenhuma sentença. Tomar só as
// primeiras palavras da primeira frase tolera âncoras multi-frase e pequenas diferenças
// de pontuação (a normalização já remove pontuação). Devolve o índice e se encontrou.
func AcharAncora(frases []Frase, ancora string) (int, bool) {
	alvo := normalizarPalavras(primeiraFrase(ancora))
	if len(alvo) == 0 {
		return 0, false
	}
	k := 6
	if len(alvo) < k {
		k = len(alvo)
	}
	chave := strings.Join(alvo[:k], " ")
	for i, f := range frases {
		if strings.Contains(strings.Join(normalizarPalavras(f.Texto), " "), chave) {
			return i, true
		}
	}
	return 0, false
}

// primeiraFrase devolve a primeira sentença de s — até a primeira palavra terminada em
// . ! ? — para casar a âncora pela sua frase de abertura mesmo quando o modelo devolve
// várias frases juntas. Se não houver pontuação final, devolve s inteiro.
func primeiraFrase(s string) string {
	var cur []string
	for _, w := range strings.Fields(s) {
		cur = append(cur, w)
		if terminaFrase(w) {
			break
		}
	}
	if len(cur) == 0 {
		return s
	}
	return strings.Join(cur, " ")
}

// bordasDoBloco resolve as bordas [ini, fim] em ms do bloco do mapa que contém o
// tempo da âncora. Se nenhum bloco contém, usa o mais próximo. Se o mapa não tem
// blocos, cai para bordas amplas em torno da âncora (não trava a delimitação).
func bordasDoBloco(mapa Mapa, ancora Frase) (iniMs, fimMs int) {
	ms := ancora.InicioMs
	melhorIni, melhorFim, achou := 0, 0, false
	melhorDist := 1 << 62
	for _, b := range mapa.Blocos {
		bi, ok1 := validacao.HmsToMs(b.InicioAprox)
		bf, ok2 := validacao.HmsToMs(b.FimAprox)
		if !ok1 || !ok2 || bf <= bi {
			continue
		}
		if ms >= bi && ms <= bf {
			return bi, bf // contém a âncora
		}
		d := distancia(ms, bi, bf)
		if d < melhorDist {
			melhorDist, melhorIni, melhorFim, achou = d, bi, bf, true
		}
	}
	if achou {
		return melhorIni, melhorFim
	}
	// Sem blocos utilizáveis: janela ampla o suficiente para caber um Short.
	return ms - duracaoMaxMs, ancora.FimMs + duracaoMaxMs
}

func distancia(ms, ini, fim int) int {
	if ms < ini {
		return ini - ms
	}
	if ms > fim {
		return ms - fim
	}
	return 0
}

// Frasear converte a transcrição "[HH:MM:SS] texto" em frases com tempo. Faz também
// a desduplicação das legendas "rolling" do YouTube (linhas que repetem o texto que
// continua na tela), reconstruindo um fluxo linear de palavras antes de dividir em
// sentenças por pontuação final (. ! ?).
func Frasear(transcricao string) []Frase {
	type tok struct {
		w  string
		ms int
	}
	var toks []tok
	var normAll []string

	for _, linha := range strings.Split(transcricao, "\n") {
		m := reLinhaTransc.FindStringSubmatch(strings.TrimSpace(linha))
		if m == nil {
			continue
		}
		ms, ok := validacao.HmsToMs(m[1])
		if !ok {
			continue
		}
		// Normaliza token a token, ignorando pontuação solta (que normaliza p/ vazio),
		// de modo que orig e norm fiquem sempre alinhados.
		var orig, norm []string
		for _, w := range strings.Fields(m[2]) {
			n := validacao.Normalizar(w)
			if n == "" {
				continue
			}
			orig = append(orig, w)
			norm = append(norm, n)
		}
		ov := sobreposicao(normAll, norm)
		for i := ov; i < len(orig); i++ {
			toks = append(toks, tok{w: orig[i], ms: ms})
			normAll = append(normAll, norm[i])
		}
	}
	if len(toks) == 0 {
		return nil
	}

	var frases []Frase
	i, n := 0, len(toks)
	for i < n {
		inicio := toks[i].ms
		var cur []string
		j := i
		for j < n {
			cur = append(cur, toks[j].w)
			if terminaFrase(toks[j].w) {
				break
			}
			j++
		}
		if j == n {
			// Fragmento final sem pontuação: incompleto — não pode encerrar um trecho.
			frases = append(frases, Frase{InicioMs: inicio, FimMs: toks[n-1].ms, Texto: strings.Join(cur, " "), Completa: false, FimLimpo: false})
			break
		}
		// FimMs = tempo da última palavra (onde o ponto final aparece). FimLimpo se a
		// próxima palavra (início da frase seguinte) só aparece num bloco POSTERIOR.
		fimLimpo := j+1 >= n || toks[j+1].ms > toks[j].ms
		frases = append(frases, Frase{InicioMs: inicio, FimMs: toks[j].ms, Texto: strings.Join(cur, " "), Completa: true, FimLimpo: fimLimpo})
		i = j + 1
	}
	return frases
}

// sobreposicao devolve o maior k tal que o sufixo de `a` (k palavras) é igual ao
// prefixo de `b` (k palavras) — o quanto a nova linha repete o fim do fluxo atual.
func sobreposicao(a, b []string) int {
	max := len(b)
	if len(a) < max {
		max = len(a)
	}
	for k := max; k > 0; k-- {
		if iguais(a[len(a)-k:], b[:k]) {
			return k
		}
	}
	return 0
}

func iguais(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func terminaFrase(palavra string) bool {
	p := strings.TrimRight(palavra, `"')]}»`)
	return strings.HasSuffix(p, ".") || strings.HasSuffix(p, "!") || strings.HasSuffix(p, "?")
}

func normalizarPalavras(s string) []string {
	return strings.Fields(validacao.Normalizar(s))
}
