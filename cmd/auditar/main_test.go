package main

import (
	"strings"
	"testing"

	"srtclean/internal/harness"
	"srtclean/internal/validacao"
)

// Transcrição rolling de teste: a frase-hook atravessa a fronteira de linha (mesmo
// padrão do bug real da Fase 5). Desduplicada, "Salvação não é um processo." começa
// em 00:00:07 e "A santificação é um processo contínuo e diário." termina em 00:00:17.
const trAudit = `[00:00:07] A palavra nos diz que a salvação é pela fé, é um ato. Salvação não
[00:00:10] é pela fé, é um ato. Salvação não
[00:00:10] é pela fé, é um ato. Salvação não é um processo. Salvação você não
[00:00:13] é um processo. Salvação você não
[00:00:13] é um processo. Salvação você não constrói sozinho jamais. A santificação
[00:00:17] constrói sozinho jamais. A santificação
[00:00:17] constrói sozinho jamais. A santificação é um processo contínuo e diário.
[00:00:45] Amém, igreja.
`

func cand(start, end string, dur float64, hook string) validacao.Candidato {
	return validacao.Candidato{Start: start, End: end, DurationSeconds: dur, Score: 90, Hook: hook}
}

func TestAuditarCandidatoFiel(t *testing.T) {
	frases := harness.Frasear(trAudit)
	probs, texto := AuditarCandidato(frases, cand("00:00:07.000", "00:00:45.000", 38, "Salvação não é um processo."))
	if len(probs) != 0 {
		t.Errorf("candidato fiel não deveria ter problemas: %v", probs)
	}
	if !strings.Contains(texto, "Salvação não é um processo") || !strings.Contains(texto, "santificação") {
		t.Errorf("texto falado incompleto: %q", texto)
	}
}

func TestAuditarCandidatoHookClipado(t *testing.T) {
	frases := harness.Frasear(trAudit)
	// start 3s DEPOIS do início real do hook (o bug que a Fase 5 antiga introduzia).
	probs, _ := AuditarCandidato(frases, cand("00:00:10.000", "00:00:45.000", 35, "Salvação não é um processo."))
	if len(probs) == 0 || !strings.Contains(strings.Join(probs, ";"), "CLIPADO") {
		t.Errorf("deveria acusar hook clipado: %v", probs)
	}
}

func TestAuditarCandidatoHookInventado(t *testing.T) {
	frases := harness.Frasear(trAudit)
	probs, _ := AuditarCandidato(frases, cand("00:00:07.000", "00:00:45.000", 38, "Esta frase o pregador nunca disse."))
	if len(probs) == 0 || !strings.Contains(strings.Join(probs, ";"), "não encontrado") {
		t.Errorf("deveria acusar hook inventado: %v", probs)
	}
}

func TestAuditarCandidatoEndNoMeioDaFala(t *testing.T) {
	frases := harness.Frasear(trAudit)
	// end em 00:00:44 não coincide com fim de frase completa nenhuma.
	probs, _ := AuditarCandidato(frases, cand("00:00:07.000", "00:00:44.000", 37, "Salvação não é um processo."))
	if len(probs) == 0 || !strings.Contains(strings.Join(probs, ";"), "fim de frase") {
		t.Errorf("deveria acusar end fora de fim de frase: %v", probs)
	}
}

func TestAuditarCandidatoDuracaoFora(t *testing.T) {
	// Frase-hook curta com end logo adiante: duração de 10s está fora de 30–60.
	frases := harness.Frasear(trAudit)
	probs, _ := AuditarCandidato(frases, cand("00:00:07.000", "00:00:17.000", 10, "Salvação não é um processo."))
	if len(probs) == 0 || !strings.Contains(strings.Join(probs, ";"), "duração") {
		t.Errorf("deveria acusar duração fora da faixa: %v", probs)
	}
}
