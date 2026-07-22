package harness

import (
	"context"
	"fmt"
	"os"
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
		} else if verr := valida([]byte(conteudo)); verr != nil {
			motivo = verr.Error() // (b) não-JSON ou (c) campos obrigatórios faltando
		} else {
			return conteudo, nil // válido
		}
		sufixo := ""
		if tentativa < MaxTentativas {
			sufixo = ", refazendo…"
		}
		LogTentativa(fmt.Sprintf("%s: tentativa %d falhou (%s)%s", fase, tentativa, motivo, sufixo))
	}
	return "", fmt.Errorf("%s: falhou após %d tentativas; último motivo: %s", fase, MaxTentativas, motivo)
}
