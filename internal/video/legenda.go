package video

import (
	"strings"

	"srtclean/internal/harness"
)

// Legenda queimada (spec-12): o texto vem do TEXTO LIMPO da Fase 3 (harness.Frasear),
// não do SRT bruto rolling do YouTube. Frasear já desduplica as legendas rolling,
// remove ">>"/duplicações e reconstrói frases com pontuação — aqui só quebramos essas
// frases em blocos curtos de 1–2 linhas, com tempo, para queimar na base do vídeo.
//
// Não reimplementamos limpeza/desduplicação: reusamos harness.Frasear como fonte única
// do texto (mesma verdade da seleção).

// BlocoLegenda é um pedaço de legenda a exibir: 1–2 linhas de texto limpo, com início e
// fim JÁ REBASEADOS ao começo do Short (0 = primeiro frame do corte).
type BlocoLegenda struct {
	InicioMs int
	FimMs    int
	Texto    string // 1..maxLinhas linhas, separadas por "\n"
}

// BlocosLegenda pega as frases limpas (harness.Frasear da transcrição inteira) que caem
// dentro do trecho [startMs, endMs] (tempos ABSOLUTOS) e as quebra em blocos de até
// maxLinhas linhas de até charsPorLinha caracteres. O tempo de cada frase é distribuído
// entre seus blocos proporcionalmente ao tamanho do texto, e tudo é rebaseado a zero
// (start do Short). Sub-sincronia por palavra é fora de escopo (spec-12): o objetivo é
// visual — texto limpo, ≤2 linhas, na base.
func BlocosLegenda(frases []harness.Frase, startMs, endMs, charsPorLinha, maxLinhas int) []BlocoLegenda {
	var out []BlocoLegenda
	for i, f := range frases {
		// A frase pertence ao trecho se COMEÇA dentro de [startMs, endMs) — a Fase 3
		// monta o candidato a partir de frases inteiras, então o start de cada frase do
		// trecho cai aqui. (O FimMs do Frasear é pouco confiável no fim — legenda rolling.)
		if f.InicioMs < startMs || f.InicioMs >= endMs {
			continue
		}
		// Janela de exibição: do início desta frase até o início da próxima (legenda
		// contígua, sem buracos), limitada ao fim do trecho. Evita janela de largura zero
		// quando a frase cabe num único timestamp (InicioMs == FimMs).
		ini := f.InicioMs
		fim := endMs
		if i+1 < len(frases) && frases[i+1].InicioMs < fim {
			fim = frases[i+1].InicioMs
		}
		if fim <= ini {
			continue
		}
		blocos := quebrarEmBlocos(f.Texto, charsPorLinha, maxLinhas)
		if len(blocos) == 0 {
			continue
		}
		total := 0
		for _, b := range blocos {
			total += len(b)
		}
		if total == 0 {
			continue
		}
		span := fim - ini
		acc := ini
		for i, b := range blocos {
			bfim := fim
			if i < len(blocos)-1 {
				bfim = acc + span*len(b)/total
			}
			if bfim <= acc {
				bfim = acc + 1
			}
			out = append(out, BlocoLegenda{InicioMs: acc - startMs, FimMs: bfim - startMs, Texto: b})
			acc = bfim
		}
	}
	return out
}

// quebrarEmBlocos faz word-wrap do texto em linhas de até charsPorLinha e agrupa as
// linhas em blocos de até maxLinhas (nunca mais que isso — spec-12: no máximo 2 linhas).
// Cada bloco devolvido tem suas linhas juntadas por "\n".
func quebrarEmBlocos(texto string, charsPorLinha, maxLinhas int) []string {
	palavras := strings.Fields(texto)
	if len(palavras) == 0 {
		return nil
	}
	var linhas []string
	linha := ""
	for _, p := range palavras {
		cand := p
		if linha != "" {
			cand = linha + " " + p
		}
		if len([]rune(cand)) > charsPorLinha && linha != "" {
			linhas = append(linhas, linha)
			linha = p
		} else {
			linha = cand
		}
	}
	if linha != "" {
		linhas = append(linhas, linha)
	}

	var blocos []string
	for i := 0; i < len(linhas); i += maxLinhas {
		j := i + maxLinhas
		if j > len(linhas) {
			j = len(linhas)
		}
		blocos = append(blocos, strings.Join(linhas[i:j], "\n"))
	}
	return blocos
}
