// Ajuste manual do corte pelo operador (spec-05 v2).
//
// O problema que isto resolve: com o "ouvir a emenda", o operador detecta um corte ruim —
// mas antes só podia REPROVAR, mesmo quando o conteúdo do trecho era bom. Desperdício de
// um trecho aproveitável e de trabalho do modelo.
//
// Contrato de tempo: os dois relógios são o MESMO. O vídeo baixado é o vídeo inteiro com
// origem 0 e o player do YouTube também conta do início do vídeo, então o getCurrentTime()
// do player é o tempo absoluto que o corte vai usar — sem conversão. (O alerta da spec-05
// sobre desalinhamento vinha do --download-sections, que não é mais usado.)
package servidor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"srtclean/internal/harness"
	"srtclean/internal/pipeline"
	"srtclean/internal/validacao"
	"srtclean/internal/videocache"
)

// FraseVizinha é uma frase em volta do trecho, para a FAIXA DE FRASES CLICÁVEL da tela.
//
// É a peça central do redesenho: clicar numa frase troca "empurrar até acertar" por
// "apontar onde é". E não é recurso novo — o servidor já encaixa o corte em fronteira de
// fala usando o Frasear, então a frase É a unidade nativa do ajuste. A faixa só expõe o que
// já existia escondido.
type FraseVizinha struct {
	InicioMs int    `json:"inicio_ms"`
	FimMs    int    `json:"fim_ms"`
	Rotulo   string `json:"rotulo"` // HH:MM:SS, sem milissegundos (ver comentário em rotulo)
	Texto    string `json:"texto"`
	Dentro   bool   `json:"dentro"` // está dentro do corte atual? (destaque na tela)
	// Falas é quantas frases da transcrição foram AGRUPADAS nesta entrada (1 = nenhuma
	// agrupada). Ver agruparPorCarimbo.
	Falas int `json:"falas"`
}

// TrechoAjustado é o resultado do recálculo de um trecho com tempos novos. É o que o
// operador vê ANTES de aprovar: ele está julgando o texto, não os números.
//
// Tempos em MILISSEGUNDOS INTEIROS. Não em float de segundos: uma sequência de empurrões de
// 0,25 s acumularia erro de ponto flutuante, e o tempo é a chave do corte. Não em string
// tampouco — string é formato de saída, não de cálculo. As strings Start/End existem só
// porque o Candidato (contrato do JSON de candidatos) as usa.
type TrechoAjustado struct {
	Indice int `json:"indice"`

	// Start/End efetivos, já encaixados/clampados. Podem diferir do que o operador marcou —
	// por isso voltam, para ele ver onde caiu de fato.
	StartMs int    `json:"start_ms"`
	EndMs   int    `json:"end_ms"`
	Start   string `json:"start"` // HH:MM:SS.000 — formato do Candidato
	End     string `json:"end"`

	// Hook RECALCULADO: a primeira frase a partir do start. Não é opcional — ao estender
	// para trás, o hook deixa de ser a frase-âncora e passa a ser a abertura de fato.
	Hook          string `json:"hook"`
	DuracaoMs     int    `json:"duracao_ms"`
	TextoFalado   string `json:"texto_falado"`
	AjustadoStart bool   `json:"ajustado_start"` // o encaixe moveu o que o operador marcou?
	AjustadoEnd   bool   `json:"ajustado_end"`

	// RegraFim diz QUEM decidiu o fim: "pausa" (fronteira do áudio) ou "legenda" (fronteira de
	// bloco, quando não há análise de pausas em disco). Vai para a tela e para o histórico —
	// sem isso, o operador não sabe qual das duas regras produziu o corte que ele está julgando.
	RegraFim string `json:"regra_fim,omitempty"`
	// DeslocamentoFimMs é quanto o encaixe MOVEU o fim pedido. A tela mostra quando é grande:
	// no caso medido seriam +3,5 s, e descobrir isso ao ouvir é pior que ler antes.
	DeslocamentoFimMs int `json:"deslocamento_fim_ms,omitempty"`

	// Vizinhanca são as frases dentro do corte mais algumas de cada lado, para a faixa
	// clicável. O cliente não precisa (nem deve) refrasear nada: uma fonte só.
	Vizinhanca []FraseVizinha `json:"vizinhanca"`

	// Aprovavel diz se este trecho pode ir ao render. Falso => Motivo explica o que falta,
	// em palavras e com números ("ficaria 64s, o máximo é 58s").
	Aprovavel bool   `json:"aprovavel"`
	Motivo    string `json:"motivo,omitempty"`
}

// LimitesPregacao é a janela da pregação informada no pedido (opcional). O ajuste é
// clampado nela: não faz sentido o operador estender o corte para dentro do louvor ou dos
// avisos, que é justamente o que o recorte da pregação exclui.
type LimitesPregacao struct {
	IniMs int
	FimMs int // 0 = sem limite superior conhecido
}

// ContextoAjuste é o que o recálculo precisa saber além dos tempos: os limites da pregação, as
// PAUSAS do áudio (quando há análise em disco) e QUAL GESTO o operador fez.
//
// O gesto importa porque as regras são DIFERENTES de propósito, e unificá-las quebraria as duas:
//
//	clique em frase  -> vai para a próxima PAUSA, sem limite de distância (é o conserto do bug:
//	                    a fronteira do bloco de legenda cai no meio da fala)
//	empurrão fino/1s -> NÃO encaixa. Se encaixasse, cada empurrão de 0,25 s voltaria para a
//	                    mesma pausa e o ajuste fino deixaria de existir
//	arraste (futuro) -> ímã só se houver pausa dentro de ~200 ms
//
// Struct em vez de mais três parâmetros porque as três informações andam juntas em todas as
// chamadas; o campo zerado é o comportamento de antes (sem pausas, sem gesto).
type ContextoAjuste struct {
	Lim    LimitesPregacao
	Pausas []videocache.Pausa
	Gesto  string
}

// recalcularTrecho é o coração do ajuste: dados tempos novos (em segundos, como vêm do
// player), devolve o trecho recalculado — hook, duração e texto REALMENTE falado.
//
// O encaixe em fronteira de fala não é conveniência, é o que mantém verdadeira a invariante
// que o auditor verifica (spec-16: o hook começa EXATAMENTE no start, Δ=0). Sem encaixe, um
// start 0,4 s antes da frase faria o auditor acusar "sobra de abertura" em todo trecho
// ajustado à mão, e a saída seria ensinar uma exceção ao auditor — pior remédio que a
// doença. De quebra, dispensa precisão do operador: ele julga de ouvido, e a fronteira de
// fala é um evento de 0,1–0,3 s.
func recalcularTrecho(frases []harness.Frase, indice, startMs, endMs int, ctx ContextoAjuste) TrechoAjustado {
	t := TrechoAjustado{Indice: indice}
	brutoIni, brutoFim := clampar(startMs, endMs, ctx.Lim)

	// falhar preenche o mínimo para a tela continuar coerente mesmo com ajuste inválido: o
	// operador precisa ver a faixa de frases para saber como consertar.
	falhar := func(ini, fim int, motivo string) TrechoAjustado {
		t.StartMs, t.EndMs = ini, fim
		t.Start, t.End = hms(ini), hms(fim)
		t.DuracaoMs = fim - ini
		t.Motivo = motivo
		t.Vizinhanca = vizinhanca(frases, ini, fim)
		return t
	}

	if brutoFim <= brutoIni {
		return falhar(brutoIni, brutoFim, "o fim precisa vir depois do início")
	}

	iniIdx, iniMs, okI := encaixarInicio(frases, brutoIni)
	fimMs, okF, regra := encaixarFimComPausas(frases, ctx, brutoFim)
	if !okI || !okF {
		return falhar(brutoIni, brutoFim, "não há fala transcrita nessa faixa para ancorar o corte")
	}
	t.RegraFim = regra
	t.DeslocamentoFimMs = fimMs - brutoFim

	t.AjustadoStart = iniMs != brutoIni
	t.AjustadoEnd = fimMs != brutoFim
	if fimMs <= iniMs {
		return falhar(iniMs, fimMs, "início e fim caíram na mesma fala; separe mais os dois pontos")
	}

	t.StartMs, t.EndMs = iniMs, fimMs
	t.Start, t.End = hms(iniMs), hms(fimMs)
	t.DuracaoMs = fimMs - iniMs

	// Hook = a frase que CONTÉM o start, não a próxima. Com a legenda adiantada, o start cai
	// dentro da frase pelo carimbo e no começo dela pelo áudio — o hook é o que se ouve.
	t.Hook = frases[iniIdx].Texto

	// Texto e destaque da faixa partem da FRONTEIRA da frase do hook, não do start efetivo:
	// com o start alguns segundos adiante do carimbo, usar o start deixaria a própria frase do
	// hook de fora do texto e sem destaque — o operador leria um texto que não começa no hook.
	iniFrase := frases[iniIdx].InicioMs
	t.TextoFalado = textoDoTrechoMs(frases, iniFrase, fimMs)
	t.Vizinhanca = vizinhanca(frases, iniFrase, fimMs)

	t.Aprovavel, t.Motivo = duracaoAceitavel(t.DuracaoMs)
	return t
}

// vizinhaAoRedor é quantas frases mostrar de cada lado do corte na faixa clicável. Poucas o
// bastante para caber na tela sem rolagem, muitas o bastante para o operador ver onde a ideia
// começa e termina.
const vizinhaAoRedor = 3

// vizinhanca devolve as frases DENTRO do corte mais algumas de cada lado. É o que alimenta a
// faixa clicável — a mudança principal do redesenho, porque troca "empurrar até acertar" por
// "apontar onde é".
func vizinhanca(frases []harness.Frase, iniMs, fimMs int) []FraseVizinha {
	if len(frases) == 0 {
		return nil
	}
	// Primeira e última frase que tocam o corte. Se o corte for degenerado (inválido), cai
	// no ponto mais próximo, para a faixa aparecer de todo modo.
	primeiro, ultimo := -1, -1
	for i, f := range frases {
		if f.InicioMs >= iniMs && f.InicioMs < fimMs {
			if primeiro < 0 {
				primeiro = i
			}
			ultimo = i
		}
	}
	if primeiro < 0 {
		primeiro, _, _ = encaixarInicio(frases, iniMs)
		ultimo = primeiro
	}

	de := max(0, primeiro-vizinhaAoRedor)
	ate := min(len(frases)-1, ultimo+vizinhaAoRedor)

	out := make([]FraseVizinha, 0, ate-de+1)
	for i := de; i <= ate; i++ {
		f := frases[i]
		out = append(out, FraseVizinha{
			InicioMs: f.InicioMs,
			FimMs:    f.FimMs,
			Rotulo:   rotulo(f.InicioMs),
			Texto:    f.Texto,
			Dentro:   f.InicioMs >= iniMs && f.InicioMs < fimMs,
			Falas:    1,
		})
	}
	return agruparPorCarimbo(out)
}

// agruparPorCarimbo junta numa entrada só as frases que compartilham o MESMO carimbo.
//
// O problema: a legenda tem resolução de 1 segundo, e duas frases que começam no mesmo
// segundo recebem carimbos idênticos ("Isso é um dos seus maiores alegados." e "Os puritanos
// acreditavam…" ambas em 00:04:21). Na faixa clicável isso produz duas linhas visualmente
// distintas que levam ao MESMO tempo: o operador clica na segunda, nada muda, e soma mais uma
// experiência de "não funcionou". Com granularidade de 1 s, é frequente.
//
// Escolhi AGRUPAR em vez de desempatar pela ordem. Desempatar exigiria atribuir à segunda
// frase um tempo que ninguém mediu — interpolado entre carimbos — e cortar ali colocaria o
// início do Short num ponto arbitrário no meio da fala, com aparência de precisão que o dado
// não tem. É a mesma falsa precisão que já rejeitamos na duração e nos rótulos.
//
// Agrupar é honesto: para efeito de CORTE aquelas frases são um bloco indivisível, e a faixa
// passa a dizer isso. O operador não clica esperando um resultado que o sistema não pode dar.
// De quebra a faixa fica mais curta, o que ajuda no layout de uma tela.
//
// A perda é real e aceitável: ele não pode começar o corte na segunda frase do bloco. Mas não
// podia antes tampouco — antes a interface fingia que podia.
func agruparPorCarimbo(fs []FraseVizinha) []FraseVizinha {
	if len(fs) < 2 {
		return fs
	}
	out := make([]FraseVizinha, 0, len(fs))
	for _, f := range fs {
		if n := len(out); n > 0 && out[n-1].InicioMs == f.InicioMs {
			out[n-1].Texto += " " + f.Texto
			out[n-1].Falas++
			if f.FimMs > out[n-1].FimMs {
				out[n-1].FimMs = f.FimMs
			}
			// Dentro do corte se QUALQUER fala do bloco estiver: o bloco é indivisível.
			out[n-1].Dentro = out[n-1].Dentro || f.Dentro
			continue
		}
		out = append(out, f)
	}
	return out
}

// duracaoAceitavel aplica a faixa de CONSTRUÇÃO (a mesma da Fase 3 — harness.DuracaoMinMs/
// MaxMs, um lugar só) e explica em palavras o que falta, com os números. "Fora da faixa"
// não ajuda ninguém a consertar; "ficaria 64s, o máximo é 58s" ajuda.
// As mensagens usam segundos INTEIROS. Exibir "64.75s" anuncia uma precisão que o sistema
// não tem — a mesma falsa precisão já rejeitada na grade de critérios. Arredonda para cima o
// que falta/sobra, para o operador não seguir a instrução e continuar fora da faixa.
func duracaoAceitavel(durMs int) (bool, string) {
	switch {
	case durMs < harness.DuracaoMinMs:
		return false, fmt.Sprintf("ficaria %ds, o mínimo é %ds — estenda %ds",
			durMs/1000, harness.DuracaoMinMs/1000, arredondarParaCima(harness.DuracaoMinMs-durMs))
	case durMs > harness.DuracaoMaxMs:
		return false, fmt.Sprintf("ficaria %ds, o máximo é %ds — encurte %ds",
			durMs/1000, harness.DuracaoMaxMs/1000, arredondarParaCima(durMs-harness.DuracaoMaxMs))
	}
	return true, ""
}

// arredondarParaCima converte ms em segundos arredondando para cima: dizer "encurte 6s"
// quando faltam 6,2 s deixaria o operador fora da faixa depois de obedecer.
func arredondarParaCima(ms int) int {
	if ms <= 0 {
		return 0
	}
	return (ms + 999) / 1000
}

// clampar mantém o ajuste dentro da pregação informada no pedido.
func clampar(iniMs, fimMs int, lim LimitesPregacao) (int, int) {
	if iniMs < lim.IniMs {
		iniMs = lim.IniMs
	}
	if lim.FimMs > 0 && fimMs > lim.FimMs {
		fimMs = lim.FimMs
	}
	if iniMs < 0 {
		iniMs = 0
	}
	if fimMs < 0 {
		fimMs = 0
	}
	return iniMs, fimMs
}

// encaixarInicio libera o início PARA FRENTE e encaixa apenas para trás — o mesmo defeito da
// fonte que motivou liberar o fim afeta as duas pontas, e tratar o início como exato deixava
// o operador sem saída.
//
// O caso real: corte em 00:20:08 e ainda se ouve "...do pelo Senhor", o rabo de uma frase
// carimbada em 00:20:05. Se os carimbos fossem exatos ela teria acabado antes. A frase
// "Todo cristão…" está marcada em 00:20:08 mas só é falada por volta de 00:20:10. O operador
// clicava "mais tarde", ia para 00:20:09, e o encaixe na fronteira MAIS PRÓXIMA o devolvia
// para 00:20:08 — o botão não fazia nada visível.
//
// A diferença crucial em relação ao fim: o HOOK continua sendo a frase que CONTÉM o start,
// não a seguinte. Com start em 00:20:10, o hook segue "Todo cristão deve estar preparado…" —
// que é o que se ouve — em vez de pular para a frase de depois. Daí a função devolver o
// ÍNDICE do hook e o start efetivo separadamente: eles deixaram de coincidir.
//
// Devolve (índice da frase do hook, start efetivo, ok).
func encaixarInicio(frases []harness.Frase, ms int) (int, int, bool) {
	// A última frase que começa em ou antes do ponto marcado é a que o contém.
	contem := -1
	for i, f := range frases {
		if f.InicioMs <= ms && (contem < 0 || f.InicioMs >= frases[contem].InicioMs) {
			contem = i
		}
	}
	if contem >= 0 {
		folga := ms - frases[contem].InicioMs
		if folga <= harness.FolgaInicioMaxMs {
			return contem, ms, true // o ouvido do operador manda
		}
		// Folga grande sem nenhuma frase no meio = vão longo (pausa). Limita, para o start
		// não cair no meio de uma fala de verdade.
		return contem, frases[contem].InicioMs + harness.FolgaInicioMaxMs, true
	}
	// Antes de qualquer frase: encaixa para FRENTE. Para trás não há o que pegar.
	if len(frases) == 0 {
		return -1, 0, false
	}
	return 0, frases[0].InicioMs, true // o Frasear devolve em ordem cronológica
}

// encaixarFim libera o fim PARA FRENTE e o encaixa apenas para trás — assimetria de
// propósito, e é o que faz o ajuste manual servir para o caso que o motivou.
//
// O defeito que o operador está consertando é o timestamp da legenda adiantar o áudio em
// 1–3 s: a palavra final é engolida. Encaixar no fim de frase mais PRÓXIMO usaria esses
// mesmos timestamps errados, devolvendo o operador à fronteira defeituosa — ele marca +2 s,
// o sistema desfaz, e o ajuste não serve para nada.
//
// A regra, então:
//
//   - fim marcado igual ou POSTERIOR a alguma fronteira de frase completa: aceito como está
//     (nunca corta fala no meio; a folga cai em silêncio ou no começo da fala seguinte);
//   - fim marcado ANTES de qualquer fronteira: encaixa para FRENTE, na próxima — para trás
//     cortaria no meio;
//   - folga maior que harness.FolgaFimMaxMs: limitada, para não virar vazamento silencioso.
//
// encaixarFimComPausas escolhe a REGRA do fim conforme o gesto, e diz qual usou.
//
// # Por que a pausa, e não o bloco da legenda
//
// Medido no culto fZGyLBofmmo: o clique em frase terminava o corte em 01:30:06, e a fala corrida
// só termina em 01:30:09.486. A fronteira do bloco de legenda cai no meio da frase falada —
// blocos rolling quebram por largura de tela. Com a pausa do áudio o mesmo clique acerta.
//
// # Sem limite de distância, de propósito
//
// A pausa certa pode estar 3,5 s adiante (foi o caso). Limitar a distância recriaria o bug:
// o corte pararia no meio da fala "porque estava longe". Se a distância jogar a duração fora da
// faixa válida, isso aparece no Motivo e o trecho fica não aprovável — visível, não silencioso.
func encaixarFimComPausas(frases []harness.Frase, ctx ContextoAjuste, ms int) (int, bool, string) {
	if ehGestoDeFrase(ctx.Gesto) {
		if p, ok := primeiraPausaApos(ctx.Pausas, ms); ok {
			return p, true, "pausa"
		}
	}
	// Sem pausas em disco (culto ainda não analisado), ou gesto que NÃO encaixa (empurrão fino,
	// que precisa valer exatamente o que o operador pediu).
	fim, ok := encaixarFim(frases, ms)
	regra := "legenda"
	if !ehGestoDeFrase(ctx.Gesto) {
		regra = "pedido" // o empurrão manda; a fronteira da legenda só limita o exagero
	}
	return fim, ok, regra
}

// ehGestoDeFrase: só o clique na faixa de frases encaixa em pausa. Os empurrões finos existem
// justamente para o operador discordar da fronteira, e encaixá-los anularia o ajuste fino.
func ehGestoDeFrase(gesto string) bool {
	return gesto == "frase-inicio" || gesto == "frase-fim"
}

// primeiraPausaApos devolve o INÍCIO da primeira pausa em ou depois de ms — o instante em que a
// fala para. É esse o fim natural de um corte: a última palavra terminou, o silêncio começou.
func primeiraPausaApos(pausas []videocache.Pausa, ms int) (int, bool) {
	melhor, achou := 0, false
	for _, p := range pausas {
		if p.InicioMs >= ms && (!achou || p.InicioMs < melhor) {
			melhor, achou = p.InicioMs, true
		}
	}
	return melhor, achou
}

func encaixarFim(frases []harness.Frase, ms int) (int, bool) {
	anterior, temAnterior := 0, false
	proxima, temProxima := 0, false
	for _, f := range frases {
		if !f.Completa {
			continue
		}
		if f.FimMs <= ms && (!temAnterior || f.FimMs > anterior) {
			anterior, temAnterior = f.FimMs, true
		}
		if f.FimMs > ms && (!temProxima || f.FimMs < proxima) {
			proxima, temProxima = f.FimMs, true
		}
	}
	if temAnterior {
		if ms-anterior <= harness.FolgaFimMaxMs {
			return ms, true // o ouvido do operador manda
		}
		return anterior + harness.FolgaFimMaxMs, true
	}
	if temProxima {
		return proxima, true
	}
	return 0, false
}

// hms formata no MESMO formato da Fase 3 ("HH:MM:SS.000") — é o contrato do Candidato, que o
// render consome. Não há perda: a transcrição limpa tem timestamps em segundos inteiros
// ([HH:MM:SS]), então toda fronteira de fala é múltiplo de 1000 ms.
// hms formata o tempo do CANDIDATO — "HH:MM:SS.mmm" — com o milissegundo REAL.
//
// Aqui morava um bug caro, e a forma dele merece registro: a função fazia
// MsParaHms(ms) + ".000". O MsParaHms trunca (ms/1000), e o ".000" colado à mão AFIRMAVA que
// a fração era zero. O ajuste do operador chega em milissegundos (o player manda float, os
// botões finos andam de 0,25 s), então cada empurrão que não atravessava a fronteira do
// segundo era jogado fora — e o render, lendo a string de volta, cortava ATÉ 999 ms antes do
// pedido, no meio da palavra.
//
// Medido em dois trechos reais do dono (2026-07-30): pediu 01:07:08.250, o arquivo saiu com
// 31,000 s; pediu 01:30:15.250, saiu com 54,000 s. Nos dois a última palavra foi cortada.
//
// É a classe da regra inviolável 2 — palavra apagada em silêncio —, só que na saída de vídeo.
// E doía justamente no recurso que existe para corrigir corte: o ajuste fino era o ÚNICO
// lugar do sistema com intenção sub-segundo (a transcrição é "[HH:MM:SS]", então os candidatos
// do modelo já são segundos inteiros).
func hms(ms int) string {
	return fmt.Sprintf("%s.%03d", validacao.MsParaHms(ms), ms%1000)
}

// rotulo é o tempo COMO SE MOSTRA a uma pessoa: "00:39:18", sem os milissegundos. O ".000" do
// contrato interno anuncia uma precisão que o sistema não tem, e na tela isso só polui.
func rotulo(ms int) string { return validacao.MsParaHms(ms) }

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// aplicarAjuste devolve uma cópia do candidato com os tempos, hook e duração do ajuste. O
// resto (score, critérios, avaliação) é preservado: o operador mudou ONDE corta, não o
// julgamento do conteúdo.
func aplicarAjuste(c validacao.Candidato, t TrechoAjustado) validacao.Candidato {
	c.Start = t.Start
	c.End = t.End
	c.Hook = t.Hook
	// DurationSeconds é o contrato do Candidato (float, em segundos). Internamente o ajuste
	// trabalha em ms inteiros; a conversão acontece só aqui, na saída.
	c.DurationSeconds = float64(t.DuracaoMs) / 1000
	return c
}

// --- Endpoint ---

// handleAjustar recalcula um trecho com os tempos que o operador marcou no player e devolve
// o resultado em JSON. É LEVE de propósito: nada de disco pesado, nada de modelo — só
// Frasear sobre a transcrição já baixada. O cliente chama a cada empurrão (com debounce), e
// sem essa resposta o operador ajustaria às cegas.
func (s *Servidor) handleAjustar(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	reg, ok := s.pedidos[id]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	// Tempos em MILISSEGUNDOS INTEIROS, não em segundos float: uma rajada de empurrões de
	// 0,25 s em float acumula erro, e o tempo é a chave do corte.
	var corpo struct {
		Indice  int `json:"indice"`
		StartMs int `json:"start_ms"`
		EndMs   int `json:"end_ms"`
		// Gesto diz COMO o operador pediu (clique em frase, empurrão fino, arraste). É o que
		// escolhe a regra do fim — ver ContextoAjuste. Vazio = sem encaixe em pausa, que é o
		// comportamento de antes (e o de um POST direto, que não é o cliente).
		Gesto string `json:"gesto"`
	}
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&corpo); err != nil {
		http.Error(w, `{"erro":"corpo inválido"}`, http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	nCands := len(reg.cands)
	lim := limitesDoPedido(reg.ped)
	idPed := reg.ped.ID
	s.mu.Unlock()
	if corpo.Indice < 0 || corpo.Indice >= nCands {
		http.Error(w, `{"erro":"trecho inexistente"}`, http.StatusBadRequest)
		return
	}

	frases := s.frasesDoPedido(idPed)
	if len(frases) == 0 {
		http.Error(w, `{"erro":"transcrição indisponível para recalcular"}`, http.StatusConflict)
		return
	}

	t := recalcularTrecho(frases, corpo.Indice, corpo.StartMs, corpo.EndMs,
		ContextoAjuste{Lim: lim, Pausas: s.pausasDoPedido(reg), Gesto: corpo.Gesto})
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(t)
}

// pausasDoPedido devolve as pausas do culto, se houver análise em disco. Sem análise devolve
// nil, e o encaixe cai na fronteira da legenda — dizendo qual regra usou, em vez de fingir.
//
// Não gera aqui: gerar exige o vídeo (6,5 s de ffmpeg), e a revisão acontece antes do download no
// fluxo atual. Quem gera é a fase que tem o arquivo em mão (ver garantirPausas).
func (s *Servidor) pausasDoPedido(reg *registro) []videocache.Pausa {
	s.mu.Lock()
	videoID := reg.ped.VideoID
	s.mu.Unlock()
	if videoID == "" {
		return nil
	}
	a, err := s.cache.LerPausas(videoID)
	if err != nil {
		return nil
	}
	o := s.opcoesPausas()
	if !a.Compativel(o.NoiseDB, o.MinMs) {
		// Receita diferente da configurada: não usar dado de origem desconhecida.
		return nil
	}
	return a.Pausas
}

// frasesDoPedido lê e fraseia a transcrição do pedido. Mesma fonte da revisão (harness.
// Frasear, o mesmo do cmd/auditar), para o texto do ajuste e o da tela nunca discordarem.
func (s *Servidor) frasesDoPedido(id string) []harness.Frase {
	b, err := os.ReadFile(filepath.Join(s.baseDir, id, "transcricao.txt"))
	if err != nil {
		return nil
	}
	return harness.Frasear(string(b))
}

// limitesDoPedido traduz a janela da pregação informada no pedido. Vazio = sem limite.
func limitesDoPedido(ped *pipeline.Pedido) LimitesPregacao {
	var lim LimitesPregacao
	if ms, ok := validacao.HmsToMs(ped.Inicio); ok {
		lim.IniMs = ms
	}
	if ms, ok := validacao.HmsToMs(ped.Fim); ok {
		lim.FimMs = ms
	}
	return lim
}

// validarAjustes recalcula no SERVIDOR cada ajuste recebido e só aceita os que passam nas
// guardas. Devolve o mapa por índice e, se algo estiver fora, o motivo pronto para a tela.
//
// Recalcular aqui é deliberado: o cliente já mostrou o resultado ao operador, mas confiar no
// que ele mandou abriria a porta para um POST manual (ou um JS desatualizado) enfiar um
// corte de 64 s no render, furando a mesma faixa que a Fase 5 aplica. Cliente é
// conveniência; servidor é a guarda.
//
// Ajuste de trecho NÃO aprovado é ignorado em silêncio — o operador pode ter mexido e
// desistido, e isso não é erro.
func (s *Servidor) validarAjustes(reg *registro, aprovados []int, recebidos []ajusteRecebido) (map[int]TrechoAjustado, string) {
	if len(recebidos) == 0 {
		return nil, ""
	}
	aprovado := make(map[int]bool, len(aprovados))
	for _, i := range aprovados {
		aprovado[i] = true
	}

	s.mu.Lock()
	nCands := len(reg.cands)
	lim := limitesDoPedido(reg.ped)
	id := reg.ped.ID
	s.mu.Unlock()

	frases := s.frasesDoPedido(id)
	if len(frases) == 0 {
		return nil, "não consegui reler a transcrição para conferir os ajustes"
	}

	out := make(map[int]TrechoAjustado)
	for _, a := range recebidos {
		if a.Indice < 0 || a.Indice >= nCands {
			return nil, "um ajuste aponta para um trecho que não existe"
		}
		if !aprovado[a.Indice] {
			continue
		}
		// SEM gesto de propósito: o que chega aqui são os tempos que o operador já viu na tela,
		// depois do encaixe. Reencaixar agora moveria o corte DEPOIS de ele aprovar — o pior
		// momento possível, porque ele aprovou um número e sairia outro.
		t := recalcularTrecho(frases, a.Indice, a.StartMs, a.EndMs, ContextoAjuste{Lim: lim})
		if !t.Aprovavel {
			return nil, fmt.Sprintf("o trecho %d foi ajustado mas %s", a.Indice+1, t.Motivo)
		}
		out[a.Indice] = t
	}
	if len(out) == 0 {
		return nil, ""
	}
	return out, ""
}
