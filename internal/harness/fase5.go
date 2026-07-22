package harness

import (
	"encoding/json"

	"srtclean/internal/validacao"
)

// Fase 5 — Validação final (100% código). Rede de segurança determinística sobre os
// candidatos avaliados: reusa internal/validacao (hook alinhado ao start, duração,
// score coerente = soma dos critérios, descarte de hook inventado) e ainda garante
// que NENHUM candidato com score 0 / critérios zerados / duração fora de 30–60 s passe.
const (
	duracaoFinalMinS = 30.0
	duracaoFinalMaxS = 60.0
)

// Fase5Validar devolve os candidatos aprovados e os descartados (com motivo).
func Fase5Validar(cands []validacao.Candidato, transcricao string) ([]validacao.Candidato, []Descarte) {
	if len(cands) == 0 {
		return nil, nil
	}
	palavras := validacao.LerTranscricao(transcricao)

	brutos, _ := json.Marshal(cands)
	doc := map[string]json.RawMessage{"candidatos": brutos}

	res, err := validacao.Processar(doc, palavras, true)
	if err != nil {
		// Sem conseguir validar, é mais seguro não entregar nada.
		return nil, []Descarte{{Motivo: "falha na validação final: " + err.Error()}}
	}

	var descs []Descarte
	// Candidatos que a validação descartou (hook inventado, end<=start etc.).
	for _, rc := range res.Candidatos {
		if rc.Descartar {
			descs = append(descs, Descarte{Ancora: rc.Hook, Motivo: motivo(rc.Problemas)})
		}
	}

	var aprovados []validacao.Candidato
	for _, m := range res.Mantidos {
		b, _ := json.Marshal(m)
		var c validacao.Candidato
		if err := json.Unmarshal(b, &c); err != nil {
			continue
		}
		switch {
		case c.Score <= 0 || c.Criteria.Soma() <= 0:
			descs = append(descs, Descarte{Ancora: c.Hook, Motivo: "score 0 / critérios não avaliados"})
		case c.DurationSeconds < duracaoFinalMinS || c.DurationSeconds > duracaoFinalMaxS:
			descs = append(descs, Descarte{Ancora: c.Hook, Motivo: "duração fora de 30–60 s"})
		default:
			aprovados = append(aprovados, c)
		}
	}
	return aprovados, descs
}

func motivo(problemas []string) string {
	if len(problemas) == 0 {
		return "descartado na validação final"
	}
	return problemas[0]
}
