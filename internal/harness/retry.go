package harness

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// Rede de retry das chamadas ao modelo (spec-08). Decisões fechadas na spec:
//   - Centralizada AQUI (uma implementação; fases 1, 2 e 4 usam a mesma função).
//   - Máximo de 3 tentativas por chamada; ao esgotar, erro com contagem + último motivo.
//   - Retry cobre FORMATO/ESTRUTURA inválidos: erro de rede/transporte, resposta não-JSON
//     e JSON com campos obrigatórios faltando. NUNCA cobre conteúdo de baixa qualidade —
//     qualidade é decisão das Fases 4 e 5, o retry não tenta "melhorar" trecho ruim.
//   - Sem backoff: o modelo é local, não há rate limit (questão em aberto da spec resolvida
//     assim; reabrir só se surgir motivo).
const MaxTentativas = 3

// LogTentativa registra cada tentativa que falhou, de forma visível ao operador. É uma
// variável para os testes poderem capturar; em produção escreve no stderr. O log de
// retry é um MEDIDOR da confiabilidade do modelo no formato (nota estratégica da spec):
// dispara raramente = modelo confiável; dispara muito = evidência para trocar de modelo.
var LogTentativa = func(msg string) { fmt.Fprintln(os.Stderr, msg) }

// PedirValidado chama o modelo e aplica `valida` ao corpo da resposta. Se a chamada
// falha (rede/HTTP) OU `valida` acusa formato/campos inválidos, refaz — até MaxTentativas.
// Sucesso: devolve o conteúdo já validado. Esgotou: erro com contagem e último motivo.
//
// `fase` é só um rótulo para o log ("Fase 1", "Fase 2", "Fase 4"). `valida` recebe o
// corpo bruto e devolve erro descrevendo o que está errado (nunca julga qualidade).
func PedirValidado(ctx context.Context, modelo ModeloLLM, fase, sistema, usuario string, maxTokens int, valida func([]byte) error) (string, error) {
	var motivo string
	for tentativa := 1; tentativa <= MaxTentativas; tentativa++ {
		conteudo, err := modelo.Completar(ctx, sistema, usuario, maxTokens)
		if err != nil {
			motivo = err.Error() // (a) transporte/rede/HTTP
		} else {
			// Descasca a resposta ANTES de validar/parsear: alguns modelos (ex.: Qwen)
			// embrulham o JSON em cerca markdown (```json … ```) ou o cercam de texto. A
			// limpeza é genérica e idempotente — JSON puro (Gemma) passa intacto.
			conteudo = descascarJSON(conteudo)
			if verr := valida([]byte(conteudo)); verr != nil {
				motivo = verr.Error() // (b) não-JSON ou (c) campos obrigatórios faltando
			} else {
				return conteudo, nil // válido (e já descascado)
			}
		}
		sufixo := ""
		if tentativa < MaxTentativas {
			sufixo = ", refazendo…"
		}
		LogTentativa(fmt.Sprintf("%s: tentativa %d falhou (%s)%s", fase, tentativa, motivo, sufixo))
	}
	return "", fmt.Errorf("%s: falhou após %d tentativas; último motivo: %s", fase, MaxTentativas, motivo)
}

// descascarJSON tira o "embrulho" que alguns modelos põem em volta do JSON, para o parse
// não tropeçar. É genérica (serve a qualquer modelo) e IDEMPOTENTE: uma string que já é
// JSON puro (o caso do Gemma) sai intacta. A ordem:
//
//  1. apara espaços/quebras nas pontas;
//  2. se há cerca de código markdown (``` ou ```json), extrai o conteúdo entre as cercas,
//     descartando um eventual rótulo de linguagem na primeira linha;
//  3. recorta do primeiro delimitador de abertura ({ ou [) ao seu fechamento correspondente
//     no fim (} ou ]), descartando texto explicativo antes/depois. Para JSON puro isso é
//     um no-op (o { já é o 1º caractere e o } o último), então a string sai intacta.
//
// Nunca falha: se não reconhecer nada, devolve a string aparada e deixa o json.Unmarshal
// (e o retry) decidirem.
func descascarJSON(s string) string {
	s = strings.TrimSpace(s)

	// (2) Cerca de código markdown.
	if i := strings.Index(s, "```"); i != -1 {
		resto := s[i+3:]
		// Pula um rótulo de linguagem na 1ª linha (ex.: "json"), que não contém JSON.
		if j := strings.IndexByte(resto, '\n'); j != -1 {
			if primeira := strings.TrimSpace(resto[:j]); primeira == "" || !strings.ContainsAny(primeira, "{[") {
				resto = resto[j+1:]
			}
		}
		// Corta na cerca de fechamento, se houver.
		if k := strings.Index(resto, "```"); k != -1 {
			resto = resto[:k]
		}
		s = strings.TrimSpace(resto)
	}

	// (3) Recorta o contêiner JSON externo, descartando texto antes/depois. O tipo é
	// definido pelo primeiro delimitador de abertura que aparecer ({ objeto ou [ lista).
	abreObj := strings.IndexByte(s, '{')
	abreArr := strings.IndexByte(s, '[')
	switch {
	case abreObj != -1 && (abreArr == -1 || abreObj < abreArr):
		if fim := strings.LastIndexByte(s, '}'); fim > abreObj {
			s = s[abreObj : fim+1]
		}
	case abreArr != -1:
		if fim := strings.LastIndexByte(s, ']'); fim > abreArr {
			s = s[abreArr : fim+1]
		}
	}

	return strings.TrimSpace(s)
}
