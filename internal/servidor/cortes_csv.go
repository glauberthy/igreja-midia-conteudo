package servidor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"srtclean/internal/validacao"
)

// Registro dos cortes aprovados — SÓ ACUMULA O DADO, não age sobre ele.
//
// A hipótese que este arquivo existe para testar: cada ajuste manual é uma MEDIÇÃO do desvio
// sistemático da legenda. O operador, ao empurrar as pontas até soar certo, está medindo quanto
// o carimbo se adianta ao áudio. Se o desvio for consistente, aplicá-lo na Fase 3 melhoraria
// todos os cortes de uma vez e tornaria o ajuste manual exceção em vez de rotina.
//
// # POR QUE REGISTRA TODOS OS APROVADOS, E NÃO SÓ OS AJUSTADOS
//
// Registrar apenas os ajustados montaria uma amostra composta só dos casos ruins. Os trechos
// aprovados SEM ajuste são justamente a evidência de que o corte estava bom, e não gerariam
// linha. O erro é grande, não marginal: com 10 aprovados, 3 ajustados em +2 s e 7 aceitos como
// estavam, a média sobre os ajustados dá 2 s e a média real dá 0,6 s. Aplicar 2 s na Fase 3
// empurraria os 7 corretos para longe demais — o remédio criaria a doença nos casos saudáveis.
//
// Registrando todo aprovado (delta 0 para os não ajustados), a média fica correta e sai de
// graça a PROPORÇÃO de cortes que precisam de ajuste — o indicador de saúde do sistema. Se
// cair de 60% para 10%, melhorou.
//
// Daí o nome ser cortes.csv e não ajustes.csv: um arquivo chamado "ajustes" convida quem o lê a
// filtrar mentalmente só os ajustados, recriando o viés que a mudança removeu.
//
// COMO LER OS NÚMEROS (dois cuidados, para quando houver amostra)
//
// (a) Os deltas são QUANTIZADOS por fronteira de frase — o corte encaixa em fala, não em tempo
// contínuo. A distribuição sai aos caroços, agrupada nas distâncias típicas entre frases, e com
// poucas amostras a média cai num vale entre dois caroços e não descreve nenhum caso real.
// Olhar a FORMA da distribuição, não só o valor central.
//
// (b) O delta mede a soma de DOIS efeitos: o adiantamento da legenda (física da fonte) e a
// preferência do operador por respiro no corte (gosto). Aplicar o total na Fase 3 embutiria o
// gosto dele como se fosse física. Pista para separar: se início e fim andarem com magnitudes
// parecidas, é sincronia; se o fim andar sistematicamente mais que o início, tem gosto no meio.
//
// Deliberadamente NÃO há correção automática nem sugestão de viés aqui. Agir sobre três pontos
// seria construir sobre ruído; e se o desvio acabar inconsistente, o dado custou nada e a
// hipótese morre com evidência em vez de opinião.
const cabecalhoCortes = "quando,pedido,indice,ajustado,start_original,start_final,delta_start_ms," +
	"end_original,end_final,delta_end_ms,duracao_original_s,duracao_final_s\n"

// registrarCortes grava uma linha por trecho APROVADO — ajustado ou não. Falha de escrita NUNCA
// quebra o pedido: é dado de pesquisa, e o Short do operador vale mais que a estatística.
func (s *Servidor) registrarCortes(reg *registro, aprovados []int, ajustes map[int]TrechoAjustado) {
	if len(aprovados) == 0 || s.cortesPath == "" {
		return
	}

	s.mu.Lock()
	idPedido := reg.ped.ID
	quando := s.agora().Format("2006-01-02T15:04:05")
	linhas := make([]string, 0, len(aprovados))
	// Ordem estável por índice: o CSV é lido por humano e comparado entre execuções.
	for i := 0; i < len(reg.cands); i++ {
		if !contemIndice(aprovados, i) {
			continue
		}
		orig := reg.cands[i]
		iniOrig, okI := validacao.HmsToMs(orig.Start)
		fimOrig, okF := validacao.HmsToMs(orig.End)
		if !okI || !okF {
			continue // candidato com tempo ilegível não rende medição confiável
		}

		// Sem ajuste: os tempos finais SÃO os originais, e os deltas são zero. É essa linha
		// que impede a média de enviesar.
		iniFinal, fimFinal := iniOrig, fimOrig
		durFinal := fimOrig - iniOrig
		if t, ok := ajustes[i]; ok {
			iniFinal, fimFinal, durFinal = t.StartMs, t.EndMs, t.DuracaoMs
		}

		// "ajustado" reflete se o corte MUDOU, não se o operador mexeu no painel: quem
		// experimenta e volta ao original está confirmando que o corte estava bom, e contar
		// isso como "precisou de ajuste" estragaria o indicador de saúde.
		ajustado := "nao"
		if iniFinal != iniOrig || fimFinal != fimOrig {
			ajustado = "sim"
		}

		linhas = append(linhas, fmt.Sprintf("%s,%s,%d,%s,%s,%s,%d,%s,%s,%d,%d,%d\n",
			quando, idPedido, i, ajustado,
			rotulo(iniOrig), rotulo(iniFinal), iniFinal-iniOrig,
			rotulo(fimOrig), rotulo(fimFinal), fimFinal-fimOrig,
			(fimOrig-iniOrig)/1000, durFinal/1000))
	}
	s.mu.Unlock()

	if len(linhas) == 0 {
		return
	}
	if err := s.anexarCortes(strings.Join(linhas, "")); err != nil {
		fmt.Fprintf(os.Stderr, "aviso: não registrei os cortes aprovados: %v\n", err)
	}
}

func contemIndice(is []int, i int) bool {
	for _, v := range is {
		if v == i {
			return true
		}
	}
	return false
}

// anexarCortes escreve em modo append, criando o arquivo com cabeçalho na primeira vez.
// Serializado pelo mesmo mutex do log de rodadas: dois pedidos concorrentes não podem
// intercalar linhas pela metade.
func (s *Servidor) anexarCortes(conteudo string) error {
	s.logMu.Lock()
	defer s.logMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.cortesPath), 0o755); err != nil {
		return err
	}
	novo := false
	if _, err := os.Stat(s.cortesPath); os.IsNotExist(err) {
		novo = true
	}
	f, err := os.OpenFile(s.cortesPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if novo {
		if _, err := f.WriteString(cabecalhoCortes); err != nil {
			return err
		}
	}
	_, err = f.WriteString(conteudo)
	return err
}
