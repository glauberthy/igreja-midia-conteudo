package servidor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"srtclean/internal/validacao"
)

// Registro dos ajustes manuais de corte — SÓ ACUMULA O DADO, não age sobre ele.
//
// A hipótese que este arquivo existe para testar: cada ajuste manual é uma MEDIÇÃO do desvio
// sistemático da legenda. O operador, ao empurrar o início e o fim até soar certo, está
// medindo quanto o carimbo se adianta ao áudio. Se o desvio for consistente ao longo de uns
// dez trechos, aplicá-lo na Fase 3 melhoraria todos os cortes de uma vez e tornaria o ajuste
// manual exceção em vez de rotina.
//
// Deliberadamente NÃO há correção automática nem sugestão de viés aqui. Agir sobre três
// pontos seria construir sobre ruído; e se o desvio acabar inconsistente, o dado custou nada
// e a hipótese morre com evidência em vez de opinião.
const cabecalhoAjustes = "quando,pedido,indice,start_original,start_ajustado,delta_start_ms," +
	"end_original,end_ajustado,delta_end_ms,duracao_original_s,duracao_ajustada_s\n"

// registrarAjustes grava uma linha por trecho ajustado. Falha de escrita NUNCA quebra o
// pedido: é dado de pesquisa, e o Short do operador vale mais que a estatística.
func (s *Servidor) registrarAjustes(reg *registro, ajustes map[int]TrechoAjustado) {
	if len(ajustes) == 0 || s.ajustesPath == "" {
		return
	}

	s.mu.Lock()
	idPedido := reg.ped.ID
	linhas := make([]string, 0, len(ajustes))
	// Ordem estável por índice: o CSV é lido por humano e comparado entre execuções.
	for i := 0; i < len(reg.cands); i++ {
		t, ok := ajustes[i]
		if !ok {
			continue
		}
		orig := reg.cands[i]
		iniOrig, okI := validacao.HmsToMs(orig.Start)
		fimOrig, okF := validacao.HmsToMs(orig.End)
		if !okI || !okF {
			continue // candidato com tempo ilegível não rende medição confiável
		}
		linhas = append(linhas, fmt.Sprintf("%s,%s,%d,%s,%s,%d,%s,%s,%d,%d,%d\n",
			s.agora().Format("2006-01-02T15:04:05"),
			idPedido, i,
			rotulo(iniOrig), rotulo(t.StartMs), t.StartMs-iniOrig,
			rotulo(fimOrig), rotulo(t.EndMs), t.EndMs-fimOrig,
			(fimOrig-iniOrig)/1000, t.DuracaoMs/1000))
	}
	s.mu.Unlock()

	if len(linhas) == 0 {
		return
	}
	if err := s.anexarAjustes(strings.Join(linhas, "")); err != nil {
		fmt.Fprintf(os.Stderr, "aviso: não registrei os ajustes de corte: %v\n", err)
	}
}

// anexarAjustes escreve em modo append, criando o arquivo com cabeçalho na primeira vez.
// Serializado pelo mesmo mutex do log de rodadas: dois pedidos concorrentes não podem
// intercalar linhas pela metade.
func (s *Servidor) anexarAjustes(conteudo string) error {
	s.logMu.Lock()
	defer s.logMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.ajustesPath), 0o755); err != nil {
		return err
	}
	novo := false
	if _, err := os.Stat(s.ajustesPath); os.IsNotExist(err) {
		novo = true
	}
	f, err := os.OpenFile(s.ajustesPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if novo {
		if _, err := f.WriteString(cabecalhoAjustes); err != nil {
			return err
		}
	}
	_, err = f.WriteString(conteudo)
	return err
}
