package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"srtclean/internal/validacao"
)

// Fase 4 — Avaliação por candidato, EM DUPLICATA. O modelo pontua os 5 critérios do
// trecho já recortado (com as regras + a Declaração Doutrinária no prompt de sistema).
// Feita 2x por candidato; o código combina as duas rodadas (nada de conta no modelo).
//
// Decisões:
//   - score final = a MENOR das duas somas (mais conservador) (spec-07).
//   - fidelidade baixa NÃO descarta (spec-11): o modelo não é confiável para VETAR
//     fidelidade (chegou a vetar citação direta da Bíblia). Em vez de vetar, MARCA o
//     trecho `requer_revisao_reforcada = true` com um motivo legível e o mantém — quem
//     decide fidelidade é o humano. O modelo levanta suspeita; não descarta.
//   - também marca revisão quando a fidelidade diverge muito entre as duas rodadas.
const (
	maxTokensAvaliacao      = 900
	limiarRevisaoFidelidade = 18 // context_fidelity (0–30) abaixo disto = marca revisão (não veta)
	divergenciaFidelidade   = 8  // diferença de fidelidade entre rodadas que exige revisão
)

// Avaliacao é a saída de UMA rodada de avaliação do modelo.
type Avaliacao struct {
	Criteria    validacao.Criteria `json:"criteria"`
	Observacoes string             `json:"observacoes"`
}

// ResultadoAvaliacao é a combinação determinística das duas rodadas. Não há mais
// "vetado" (spec-11): fidelidade baixa vira marcação de revisão, não descarte.
type ResultadoAvaliacao struct {
	Score         int                // = menor das duas somas
	Criteria      validacao.Criteria // do round mais conservador (menor soma)
	Observacoes   string
	RequerRevisao bool
	MotivoRevisao string // motivo legível quando RequerRevisao (para o operador)
}

// Fase4Avaliar faz UMA rodada de avaliação do trecho. promptSistema traz as regras de
// pontuação e a Declaração Doutrinária; trechoTexto é o texto do trecho já recortado.
func Fase4Avaliar(ctx context.Context, modelo ModeloLLM, promptSistema, trechoTexto string) (Avaliacao, error) {
	conteudo, err := PedirValidado(ctx, modelo, "Fase 4", promptSistema, trechoTexto, maxTokensAvaliacao, validaAvaliacao)
	if err != nil {
		return Avaliacao{}, err
	}
	var a Avaliacao
	_ = json.Unmarshal([]byte(conteudo), &a) // já validado por validaAvaliacao
	return a, nil
}

// validaAvaliacao é a validação de FORMATO da Fase 4 (spec-08): JSON com o objeto
// `criteria` contendo os 5 campos numéricos. Usa ponteiros para distinguir "campo
// ausente" de "valor 0" — retry só cobre estrutura faltando, NUNCA nota baixa (uma
// nota 0 legítima é conteúdo ruim, decisão das Fases 4/5, não motivo de refazer).
func validaAvaliacao(b []byte) error {
	var doc struct {
		Criteria *struct {
			ContextFidelity *int `json:"context_fidelity"`
			PastoralValue   *int `json:"pastoral_value"`
			Completeness    *int `json:"completeness"`
			OpeningStrength *int `json:"opening_strength"`
			FormatFit       *int `json:"format_fit"`
		} `json:"criteria"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return fmt.Errorf("JSON inválido: %w", err)
	}
	if doc.Criteria == nil {
		return fmt.Errorf("faltando objeto criteria")
	}
	c := doc.Criteria
	var faltando []string
	if c.ContextFidelity == nil {
		faltando = append(faltando, "context_fidelity")
	}
	if c.PastoralValue == nil {
		faltando = append(faltando, "pastoral_value")
	}
	if c.Completeness == nil {
		faltando = append(faltando, "completeness")
	}
	if c.OpeningStrength == nil {
		faltando = append(faltando, "opening_strength")
	}
	if c.FormatFit == nil {
		faltando = append(faltando, "format_fit")
	}
	if len(faltando) > 0 {
		return fmt.Errorf("criteria sem campos: %s", strings.Join(faltando, ", "))
	}
	return nil
}

// CombinarAvaliacoes (LÓGICA PURA) junta as duas rodadas conforme as decisões da spec.
func CombinarAvaliacoes(a, b Avaliacao) ResultadoAvaliacao {
	somaA, somaB := a.Criteria.Soma(), b.Criteria.Soma()

	var r ResultadoAvaliacao
	if somaA <= somaB { // a menor soma é a mais conservadora
		r.Score, r.Criteria, r.Observacoes = somaA, a.Criteria, a.Observacoes
	} else {
		r.Score, r.Criteria, r.Observacoes = somaB, b.Criteria, b.Observacoes
	}

	// Fidelidade baixa em QUALQUER rodada não veta mais (spec-11): marca revisão.
	fidBaixa := a.Criteria.ContextFidelity < limiarRevisaoFidelidade ||
		b.Criteria.ContextFidelity < limiarRevisaoFidelidade

	// Divergência grande de fidelidade entre as rodadas também pede revisão (spec-07).
	difFid := a.Criteria.ContextFidelity - b.Criteria.ContextFidelity
	if difFid < 0 {
		difFid = -difFid
	}
	divergente := difFid > divergenciaFidelidade

	r.RequerRevisao = fidBaixa || divergente
	switch { // o motivo de fidelidade tem prioridade (é o alerta mais importante)
	case fidBaixa:
		r.MotivoRevisao = "possível problema de fidelidade — revisar"
	case divergente:
		r.MotivoRevisao = "avaliações divergentes de fidelidade — revisar"
	}

	return r
}
