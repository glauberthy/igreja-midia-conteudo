package servidor

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"srtclean/internal/transcricao"
)

// Instrumentação de tempo por pedido (auditoria de desempenho). Grava uma LINHA POR PEDIDO
// em resultados/tempos.csv (append), acumulando histórico para responder com dados: o ciclo
// está em torno de X? quando ficou mais lento, QUAL etapa cresceu e por quê?
//
// CSV (e não markdown) porque o objetivo é comparar/agregar — média e variação sobre muitos
// pedidos, não ler um caso isolado.
//
// O tempo AGUARDANDO APROVAÇÃO é medido, mas fica em coluna separada e FORA do total de
// máquina: é tempo humano (o operador revisando), não desempenho do sistema. Misturar os
// dois faria um pedido revisado no dia seguinte parecer uma falha de performance.

// retriesObservados conta os retries (modelo e download) para atribuí-los ao pedido em
// curso. O servidor processa um pedido por vez (fila simples, spec-05), então a diferença
// entre o início e o fim do pedido é a contagem daquele pedido. O cmd/servidor liga os
// logs de retry aqui — ver ContarRetry.
var retriesObservados atomic.Int64

// ContarRetry incrementa o contador de retries. Chamado pelos hooks de log do harness
// (LogTentativa) e do download (LogTentativaDownload), ligados no cmd/servidor.
func ContarRetry() { retriesObservados.Add(1) }

// logTempos escreve o resumo de desempenho/limpeza. É um CAMPO do Servidor (injetável por
// Opcoes.LogTempos), não uma variável global: as goroutines das fases escrevem por aqui, e
// um global trocado/restaurado por teste vira corrida com a goroutine que ainda está
// finalizando (pego pelo -race). Nil = stderr, o log do servidor.
func (s *Servidor) logTempos(msg string) {
	if s.logTemposFn != nil {
		s.logTemposFn(msg)
		return
	}
	fmt.Fprintln(os.Stderr, msg)
}

// duracaoJanelaS devolve a duração da janela [inicio, fim] do pedido em segundos (0 se os
// tempos não parsearem). É o principal previsor de custo: sermão maior = transcrição maior
// = seleção mais lenta.
func duracaoJanelaS(inicio, fim string) int {
	i, oki := transcricao.HmsToMs(inicio)
	f, okf := transcricao.HmsToMs(fim)
	if !oki || !okf || f <= i {
		return 0
	}
	return (f - i) / 1000
}

// Metricas acumula os tempos e o contexto de um pedido. Os campos de duração são em
// milissegundos (precisão suficiente e fácil de agregar).
type Metricas struct {
	ID     string
	Titulo string
	Quando time.Time

	// Contexto que explica variação entre pedidos.
	DuracaoSermaoS    int   // janela [inicio, fim] do pedido, em segundos
	TokensTranscricao int   // aproximado (bytes/4) da transcrição desduplicada
	NumCandidatos     int   // gerados pela seleção
	NumAprovados      int   // aprovados pelo operador
	BytesVideo        int64 // tamanho do video.mp4 baixado
	Retries           int   // retries de modelo + download durante o pedido

	// Desfecho. Pedidos que FALHAM também entram no CSV: o tempo gasto até a falha é real
	// e o operador vai refazer — registrar só o sucesso deixaria a média otimista.
	// VideoReusado marca que o vídeo já estava em disco (não houve download). Sem esta coluna,
	// um pedido reaproveitado entraria na média de download como se tivesse baixado em 0s.
	VideoReusado bool
	// Retomado marca o pedido que veio do `-retomar`: ele PULA a fase leve (legenda e seleção),
	// então tem legenda_s e selecionar_s zerados por construção, não por rapidez.
	//
	// Existe pela mesma razão da coluna VideoReusado e do registro dos não-ajustados no
	// cortes.csv: sem ela, qualquer média de selecionar_s misturaria ciclos que nunca
	// selecionaram, e quem for ler o CSV não teria como saber quais filtrar.
	Retomado  bool
	Completou bool   // true = ciclo completo; false = terminou em erro
	Erro      string // motivo, quando não completou

	// Etapas (ms).
	BaixarLegendaMs int64
	SelecionarMs    int64
	ValidarMs       int64
	AguardandoMs    int64 // TEMPO HUMANO — fora do total de máquina
	BaixarVideoMs   int64
	RenderizarMs    int64

	// Controle interno.
	inicioEtapa   time.Time
	retriesInicio int64
}

// IniciarPedido marca o começo e fixa a base do contador de retries.
func (m *Metricas) IniciarPedido(agora time.Time) {
	m.Quando = agora
	m.inicioEtapa = agora
	m.retriesInicio = retriesObservados.Load()
}

// marcar devolve o tempo decorrido desde a última marcação e reinicia o cronômetro.
func (m *Metricas) marcar(agora time.Time) int64 {
	if m.inicioEtapa.IsZero() {
		m.inicioEtapa = agora
		return 0
	}
	d := agora.Sub(m.inicioEtapa).Milliseconds()
	m.inicioEtapa = agora
	if d < 0 {
		return 0
	}
	return d
}

// FecharRetries calcula quantos retries dispararam durante o pedido.
func (m *Metricas) FecharRetries() {
	if n := retriesObservados.Load() - m.retriesInicio; n > 0 {
		m.Retries = int(n)
	}
}

// TotalMaquinaMs é a soma das etapas de SISTEMA (exclui a espera humana) — é o número que
// responde "quanto o ciclo leva de verdade".
func (m *Metricas) TotalMaquinaMs() int64 {
	return m.BaixarLegendaMs + m.SelecionarMs + m.ValidarMs + m.BaixarVideoMs + m.RenderizarMs
}

// RenderPorShortMs é a média por Short (o render processa os aprovados numa passada; esta é
// a média, não uma medição individual — nomeada assim na coluna para não enganar).
func (m *Metricas) RenderPorShortMs() int64 {
	if m.NumAprovados <= 0 {
		return 0
	}
	return m.RenderizarMs / int64(m.NumAprovados)
}

const cabecalhoTempos = "quando,pedido,titulo,sermao_s,transcricao_tokens,candidatos,aprovados," +
	"video_mb,retries,baixar_legenda_s,selecionar_s,validar_s,baixar_video_s,renderizar_s," +
	"render_por_short_s,total_maquina_s,aguardando_humano_s,video_reusado,completou,erro,retomado\n"

// LinhaCSV formata o pedido como uma linha do arquivo de auditoria. Tempos em segundos com
// 1 casa (mais legível que ms para comparar a olho, e suficiente para média).
func (m *Metricas) LinhaCSV() string {
	seg := func(ms int64) string { return fmt.Sprintf("%.1f", float64(ms)/1000) }
	return strings.Join([]string{
		m.Quando.UTC().Format(time.RFC3339),
		m.ID,
		csvCampo(m.Titulo),
		fmt.Sprintf("%d", m.DuracaoSermaoS),
		fmt.Sprintf("%d", m.TokensTranscricao),
		fmt.Sprintf("%d", m.NumCandidatos),
		fmt.Sprintf("%d", m.NumAprovados),
		fmt.Sprintf("%.1f", float64(m.BytesVideo)/(1024*1024)),
		fmt.Sprintf("%d", m.Retries),
		seg(m.BaixarLegendaMs),
		seg(m.SelecionarMs),
		seg(m.ValidarMs),
		seg(m.BaixarVideoMs),
		seg(m.RenderizarMs),
		seg(m.RenderPorShortMs()),
		seg(m.TotalMaquinaMs()),
		seg(m.AguardandoMs),
		simNao(m.VideoReusado),
		completouTexto(m.Completou),
		csvCampo(m.Erro),
		// COLUNA NOVA VAI SEMPRE NO FIM. Não é estética: o cabeçalho é escrito uma única vez, na
		// criação do arquivo, então um CSV que já existe precisa ser MIGRADO quando uma coluna
		// entra. Com a coluna no fim, migrar é acrescentar campo vazio no fim de cada linha
		// antiga — operação trivial e segura. Inserir no meio exigiria adivinhar posição, que é
		// como se estraga histórico. Ver alinharCabecalho.
		simNao(m.Retomado),
	}, ",") + "\n"
}

// csvCampo protege o campo de texto (títulos têm vírgula e barra).
func csvCampo(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if strings.ContainsAny(s, ",\"") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

// Resumo é a linha legível mostrada no log do servidor ao final do pedido.
func (m *Metricas) Resumo() string {
	s := func(ms int64) float64 { return float64(ms) / 1000 }
	if !m.Completou {
		return fmt.Sprintf(
			"tempos [%s] FALHOU após %.1fs de máquina (legenda %.1fs + selecionar %.1fs + "+
				"baixar vídeo %.1fs + renderizar %.1fs) | motivo: %s",
			m.ID, s(m.TotalMaquinaMs()), s(m.BaixarLegendaMs), s(m.SelecionarMs),
			s(m.BaixarVideoMs), s(m.RenderizarMs), m.Erro)
	}
	return fmt.Sprintf(
		"tempos [%s] TOTAL DE MÁQUINA %.1fs = legenda %.1fs + selecionar %.1fs + validar %.1fs "+
			"+ baixar vídeo %.1fs + renderizar %.1fs (%.1fs/short) | espera humana %.1fs "+
			"| sermão %dmin, ~%d tok, %d cand → %d aprovados, vídeo %.0f MB, %d retries",
		m.ID, s(m.TotalMaquinaMs()), s(m.BaixarLegendaMs), s(m.SelecionarMs), s(m.ValidarMs),
		s(m.BaixarVideoMs), s(m.RenderizarMs), s(m.RenderPorShortMs()), s(m.AguardandoMs),
		m.DuracaoSermaoS/60, m.TokensTranscricao, m.NumCandidatos, m.NumAprovados,
		float64(m.BytesVideo)/(1024*1024), m.Retries)
}

// gravarTempos anexa a linha do pedido ao CSV de auditoria (criando o cabeçalho na primeira
// vez). É auxiliar: falha de I/O só emite aviso, nunca interrompe o pedido.
// alinharCabecalho conserta um CSV cujo cabeçalho é de uma versão anterior à coluna atual.
//
// Por que precisa existir: o cabeçalho é escrito UMA vez, na criação do arquivo. Quando a
// coluna `retomado` foi acrescentada, os arquivos que já existiam ficaram com 20 nomes e
// passaram a receber linhas de 21 campos — desalinhados em silêncio. Aconteceu de verdade, e
// foi visto olhando o CSV depois de uma medição.
//
// É o mesmo argumento que justificou consertar as linhas de retomada: este arquivo é o
// INSTRUMENTO com que se mede o ganho do cache. Dado desalinhado é pior que dado ausente,
// porque parece dado.
//
// A migração é conservadora de propósito: só age quando o cabeçalho antigo é PREFIXO do atual
// (colunas foram acrescentadas no fim, que é a única mudança que fazemos). Qualquer outra
// diferença — coluna renomeada, removida, reordenada — só avisa e não toca no arquivo, porque
// aí adivinhar o alinhamento é que estragaria o histórico.
func (s *Servidor) alinharCabecalho() {
	b, err := os.ReadFile(s.temposPath)
	if err != nil || len(b) == 0 {
		return
	}
	linhas := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	atual := strings.TrimRight(cabecalhoTempos, "\n")
	if linhas[0] == atual {
		return // já alinhado: o caso normal, e sai sem reescrever nada
	}
	antigas := strings.Split(linhas[0], ",")
	novas := strings.Split(atual, ",")
	if len(antigas) >= len(novas) || !strings.HasPrefix(atual, linhas[0]+",") {
		fmt.Fprintf(os.Stderr, "aviso: o cabeçalho de %s não é uma versão anterior do atual "+
			"(colunas renomeadas ou removidas?). Não vou reescrever o arquivo — mova-o à mão "+
			"para o histórico não sair desalinhado.\n", s.temposPath)
		return
	}
	// A partir daqui usa encoding/csv, não split(","). Dois motivos concretos, e o primeiro é
	// correção, não robustez teórica:
	//
	//  1. a coluna `erro` é texto livre e vai ENTRE ASPAS quando tem vírgula (há linhas assim no
	//     arquivo real). Contar vírgulas nessas linhas dá campo a mais, e a linha não seria
	//     completada — desalinhamento onde a migração devia consertar;
	//  2. linhas já no formato NOVO podem conviver com cabeçalho antigo (foi o que aconteceu).
	//     Completar cegamente todas daria uma vírgula sobrando justamente nelas.
	//
	// Então: cada linha é completada até o número de campos do cabeçalho novo, e quem já está
	// completa não é tocada.
	leitor := csv.NewReader(strings.NewReader(string(b)))
	// FieldsPerRecord = -1: o padrão do csv.Reader EXIGE que toda linha tenha o mesmo número de
	// campos da primeira — e um arquivo com linhas de tamanhos diferentes é exatamente o que
	// esta função existe para consertar. Com o padrão, ela recusava o arquivo que devia migrar.
	leitor.FieldsPerRecord = -1
	reg, err := leitor.ReadAll()
	if err != nil {
		fmt.Fprintf(os.Stderr, "aviso: %s não é um CSV legível (%v); não vou mexer nele\n",
			s.temposPath, err)
		return
	}
	var buf strings.Builder
	w := csv.NewWriter(&buf)
	completadas := 0
	for i, linha := range reg {
		if i == 0 {
			linha = novas // troca o cabeçalho pelo atual
		} else if len(linha) < len(novas) {
			// Campos vazios no fim: o valor é DESCONHECIDO para as linhas antigas, e vazio é
			// honesto. Escrever "nao" afirmaria algo que ninguém mediu.
			for len(linha) < len(novas) {
				linha = append(linha, "")
			}
			completadas++
		}
		if err := w.Write(linha); err != nil {
			fmt.Fprintf(os.Stderr, "aviso: não alinhei %s: %v\n", s.temposPath, err)
			return
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		fmt.Fprintf(os.Stderr, "aviso: não alinhei %s: %v\n", s.temposPath, err)
		return
	}
	if err := os.WriteFile(s.temposPath, []byte(buf.String()), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "aviso: não alinhei o cabeçalho de %s: %v\n", s.temposPath, err)
		return
	}
	fmt.Fprintf(os.Stderr, "tempos.csv: cabeçalho atualizado (+%d coluna(s)); %d linha(s) antiga(s) "+
		"completada(s) com campo vazio no fim\n", len(novas)-len(antigas), completadas)
}

func (s *Servidor) gravarTempos(m *Metricas) {
	if s.temposPath == "" {
		return
	}
	s.logMu.Lock()
	defer s.logMu.Unlock()

	if dir := filepath.Dir(s.temposPath); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "aviso: não criei o diretório do CSV de tempos: %v\n", err)
			return
		}
	}
	novo := false
	if fi, err := os.Stat(s.temposPath); err != nil || fi.Size() == 0 {
		novo = true
	}
	if !novo {
		s.alinharCabecalho()
	}
	f, err := os.OpenFile(s.temposPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aviso: não abri o CSV de tempos: %v\n", err)
		return
	}
	defer f.Close()
	if novo {
		f.WriteString(cabecalhoTempos)
	}
	if _, err := f.WriteString(m.LinhaCSV()); err != nil {
		fmt.Fprintf(os.Stderr, "aviso: não escrevi no CSV de tempos: %v\n", err)
	}
}

// completouTexto formata o desfecho para o CSV (sim/nao — fácil de filtrar com awk/grep).
func simNao(b bool) string {
	if b {
		return "sim"
	}
	return "nao"
}

func completouTexto(ok bool) string {
	if ok {
		return "sim"
	}
	return "nao"
}
