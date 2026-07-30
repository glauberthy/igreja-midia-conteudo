package servidor

import (
	"srtclean/internal/pipeline"
)

// MotivoEncerrado é o texto gravado no CSV quando o servidor cai com pedido em curso.
const MotivoEncerrado = "servidor encerrado com o pedido em curso"

// RegistrarAbandonados grava no tempos.csv uma linha por pedido que NÃO chegou ao fim,
// marcando completou=nao e o motivo. Deve ser chamada no encerramento do servidor.
//
// # Por que abandono precisa aparecer
//
// Só pedidos que terminam (concluído ou erro) escreviam no CSV, porque a linha sai do
// finalizarPedido. Então falha era registrada e ABANDONO era invisível — o servidor derrubado
// no meio de uma seleção não deixava rastro nenhum.
//
// É o mesmo viés de amostra que o cortes.csv tinha ao registrar só os trechos AJUSTADOS: a
// medição passa a conter apenas os ciclos que chegaram ao fim, e a média mente para o lado
// otimista. Pior aqui, porque o abandono é o dado MAIS informativo que estava sumindo: um
// ciclo interrompido é sintoma de algo ruim demais para terminar — travou, demorou além do
// aceitável, ou o operador desistiu.
//
// Aconteceu de verdade nesta rodada: um pedido de medição (web-20260729-213852-2) foi
// abandonado quando o servidor caiu para uma correção, e desapareceu do CSV sem deixar sinal.
//
// # Por que reusa o finalizarPedido
//
// A linha do CSV é escrita em UM lugar só (finalizarPedido → gravarTempos), e este código
// chama exatamente esse caminho. Escrever a linha aqui à mão criaria um segundo formatador de
// linha — a duplicação que acabou de custar duas quebras no cabeçalho do mesmo arquivo.
//
// Idempotente: o finalizarPedido zera reg.metricas ao finalizar, então um pedido já registrado
// (concluído, com erro, ou por uma chamada anterior desta função) não entra de novo.
func (s *Servidor) RegistrarAbandonados(motivo string) int {
	if motivo == "" {
		motivo = MotivoEncerrado
	}
	// Coleta sob lock e finaliza fora: o finalizarPedido pega o mesmo mutex.
	s.mu.Lock()
	var emCurso []*registro
	for _, reg := range s.pedidos {
		if !estadoTerminal(reg.ped.Status) && reg.metricas != nil {
			reg.ped.Status = pipeline.EstadoErro
			reg.ped.Erro = motivo
			emCurso = append(emCurso, reg)
		}
	}
	s.mu.Unlock()

	for _, reg := range emCurso {
		s.finalizarPedido(reg, motivo)
	}
	return len(emCurso)
}
