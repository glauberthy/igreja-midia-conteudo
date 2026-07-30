package servidor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// HISTÓRICO DE AÇÕES DO AJUSTE — instrumento de diagnóstico, não auditoria.
//
// # O bug que ele existe para separar
//
// O operador relata que o áudio do Short não corresponde ao trecho que ele selecionou: às vezes
// passa, às vezes falta. Há duas causas possíveis, e elas pedem soluções OPOSTAS:
//
//	(a) o sistema aplicou fielmente o que ele escolheu, e o áudio ainda não bate
//	    -> o culpado é o carimbo da legenda, e o alinhamento forçado (Rota D) é a única saída;
//	(b) o aplicado divergiu do escolhido (encaixe ou clamp mexeram)
//	    -> é bug nosso, corrigível hoje.
//
// Sem o registro do PAR — o que o operador pediu e o que o sistema aplicou — as duas são
// indistinguíveis, e a discussão vira opinião. Com ele, `delta_ms` responde: zero em toda ação
// significa (a); diferente de zero aponta exatamente qual encaixe agiu e de quanto.
//
// # Por que um arquivo NOVO, e não uma coluna no cortes.csv
//
// A régua de simplicidade pede a versão direta, e ela não serve aqui: a UNIDADE dos dois
// arquivos é diferente. O cortes.csv tem UMA linha por trecho aprovado (original contra final);
// o histórico tem N linhas por trecho — uma por ação. Enfiar as duas coisas no mesmo arquivo
// mudaria o significado de cada linha e quebraria todo leitor atual do cortes.csv, inclusive o
// cabeçalho que já quebrou duas vezes neste projeto. Arquivo ao lado, mesma pasta, mesmo estilo.
//
// O cortes.csv continua respondendo "quanto o corte final andou"; este responde "por quais
// passos, e o sistema obedeceu?".
//
// # O terceiro dado: o que o operador OUVIU
//
// `pedido` e `aplicado` provam fidelidade do sistema; nenhum dos dois mede o desvio REAL entre
// carimbo e áudio. Esse número só existe no ouvido do operador — e é ele que decide entre um
// deslocamento fixo na Fase 3 e a Rota D. Por isso a ação `ouvido` carrega uma frase livre
// ("faltou ~1s no fim"), opcional e de uma linha: se der atrito, ninguém usa, e um campo vazio
// não mede nada.

// Acao é uma ação do operador no painel de ajuste, como o cliente a registra e envia junto do
// POST /aprovar. Estrutura chapada de propósito: é log, não modelo de domínio.
type Acao struct {
	Indice int    `json:"indice"`
	Seq    int    `json:"seq"`  // ordem dentro do trecho, como aconteceu
	Tipo   string `json:"tipo"` // frase-inicio, frase-fim, fino-inicio, passo-fim, restaurar, ouvido
	// PedidoMs é o tempo que o operador pediu para a ponta que ele mexeu (0 em restaurar/ouvido).
	PedidoMs int `json:"pedido_ms"`
	// AplicadoMs é o que o servidor devolveu para a MESMA ponta, depois de encaixe e clamp.
	// Ausente (0 com AplicadoOk falso) quando a ação foi substituída por outra antes de o
	// recálculo voltar — o debounce junta empurrões rápidos, e inventar um valor aqui seria pior
	// que deixar em branco.
	AplicadoMs int  `json:"aplicado_ms"`
	AplicadoOk bool `json:"aplicado_ok"`
	// InicioMs/FimMs são o trecho resultante depois da ação (o estado, não a ponta).
	InicioMs  int    `json:"inicio_ms"`
	FimMs     int    `json:"fim_ms"`
	DuracaoMs int    `json:"duracao_ms"`
	Frase     string `json:"frase"`  // texto da frase clicada (só nas ações de clique)
	Ouvido    string `json:"ouvido"` // o que o operador percebeu (só na ação `ouvido`)
	// Regra é QUEM decidiu a ponta que a ação moveu: "pausa" (encaixe do clique em frase), "ima"
	// (arraste arredondado para a pausa a menos de 200 ms), "pedido" (o pulso valeu) ou "legenda"
	// (sem análise de pausas em disco).
	//
	// É o que separa PULSO de ENCAIXE na evidência: com pedido_ms, aplicado_ms e regra na mesma
	// linha, dá para responder "o operador arrastou até ali ou o sistema arredondou?" — pergunta
	// que o log não respondia e que decide se o desvio é nosso ou da legenda.
	Regra string `json:"regra"`
}

// colunasAcoes é a FONTE ÚNICA das colunas: nome e valor na mesma entrada. Mesmo motivo do
// colunasTempos — o cabeçalho de um CSV deste projeto já divergiu das linhas duas vezes, e a
// correção que funcionou não foi migração, foi ter uma lista só.
var colunasAcoes = []struct {
	nome  string
	valor func(linhaAcao) string
}{
	{"quando", func(l linhaAcao) string { return l.quando }},
	{"pedido", func(l linhaAcao) string { return l.pedido }},
	{"indice", func(l linhaAcao) string { return fmt.Sprintf("%d", l.a.Indice) }},
	{"seq", func(l linhaAcao) string { return fmt.Sprintf("%d", l.a.Seq) }},
	{"tipo", func(l linhaAcao) string { return csvCampo(l.a.Tipo) }},
	{"decisao", func(l linhaAcao) string { return l.decisao }},
	{"pedido_ms", func(l linhaAcao) string { return msOuVazio(l.a.PedidoMs, l.a.PedidoMs > 0) }},
	{"aplicado_ms", func(l linhaAcao) string { return msOuVazio(l.a.AplicadoMs, l.a.AplicadoOk) }},
	// delta_ms é a coluna que separa as duas causas: 0 = o sistema obedeceu; != 0 = encaixe ou
	// clamp mexeram, e de quanto. Derivada aqui, num lugar só.
	{"delta_ms", func(l linhaAcao) string {
		if !l.a.AplicadoOk || l.a.PedidoMs <= 0 {
			return ""
		}
		return fmt.Sprintf("%d", l.a.AplicadoMs-l.a.PedidoMs)
	}},
	{"inicio", func(l linhaAcao) string { return rotulo(l.a.InicioMs) }},
	{"fim", func(l linhaAcao) string { return rotulo(l.a.FimMs) }},
	{"duracao_s", func(l linhaAcao) string { return fmt.Sprintf("%.2f", float64(l.a.DuracaoMs)/1000) }},
	{"regra", func(l linhaAcao) string { return csvCampo(l.a.Regra) }},
	{"frase", func(l linhaAcao) string { return csvCampo(l.a.Frase) }},
	{"ouvido", func(l linhaAcao) string { return csvCampo(l.a.Ouvido) }},
}

// linhaAcao junta a ação ao contexto do pedido (o que não vem do cliente).
type linhaAcao struct {
	quando  string
	pedido  string
	decisao string // aprovado | reprovado (a decisão do trecho a que a ação pertence)
	a       Acao
}

func msOuVazio(ms int, ok bool) string {
	if !ok {
		return ""
	}
	return fmt.Sprintf("%d", ms)
}

// cabecalhoAcoes é DERIVADO de colunasAcoes.
var cabecalhoAcoes = func() string {
	nomes := make([]string, len(colunasAcoes))
	for i, c := range colunasAcoes {
		nomes[i] = c.nome
	}
	return strings.Join(nomes, ",") + "\n"
}()

func (l linhaAcao) csv() string {
	campos := make([]string, len(colunasAcoes))
	for i, c := range colunasAcoes {
		campos[i] = c.valor(l)
	}
	return strings.Join(campos, ",") + "\n"
}

// registrarAcoes persiste o histórico enviado pelo cliente. Chamado no /aprovar: é ali que o
// operador fecha as decisões, e é o único momento em que o servidor sabe o que foi aprovado.
//
// Falha de escrita NUNCA quebra o pedido — é evidência de diagnóstico, e o Short do operador vale
// mais que o log (mesma política do cortes.csv).
func (s *Servidor) registrarAcoes(reg *registro, acoes []Acao, aprovados []int) {
	if len(acoes) == 0 || s.acoesPath == "" {
		return
	}
	s.mu.Lock()
	idPedido := reg.ped.ID
	quando := s.agora().Format(time.RFC3339)
	var linhas []string
	for _, a := range acoes {
		decisao := "reprovado"
		if contemIndice(aprovados, a.Indice) {
			decisao = "aprovado"
		}
		linhas = append(linhas, linhaAcao{quando: quando, pedido: idPedido, decisao: decisao, a: a}.csv())
	}
	s.mu.Unlock()

	if err := s.anexarAcoes(strings.Join(linhas, "")); err != nil {
		fmt.Fprintf(os.Stderr, "aviso: não registrei o histórico de ações: %v\n", err)
	}
}

// anexarAcoes escreve em append, criando com cabeçalho na primeira vez. Serializado pelo mesmo
// mutex dos outros arquivos de resultado: dois pedidos concorrentes não podem intercalar linhas.
func (s *Servidor) anexarAcoes(conteudo string) error {
	if conteudo == "" {
		return nil
	}
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.acoesPath), 0755); err != nil {
		return err
	}
	novo := false
	if _, err := os.Stat(s.acoesPath); os.IsNotExist(err) {
		novo = true
	}
	f, err := os.OpenFile(s.acoesPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if novo {
		if _, err := f.WriteString(cabecalhoAcoes); err != nil {
			return err
		}
	}
	_, err = f.WriteString(conteudo)
	return err
}
