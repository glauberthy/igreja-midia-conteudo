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
	"math"
	"net/http"
	"os"
	"path/filepath"

	"srtclean/internal/harness"
	"srtclean/internal/pipeline"
	"srtclean/internal/validacao"
)

// TrechoAjustado é o resultado do recálculo de um trecho com tempos novos. É o que o
// operador vê ANTES de aprovar: ele está julgando o texto, não os números.
type TrechoAjustado struct {
	Indice int `json:"indice"`

	// Start/End efetivos, JÁ ENCAIXADOS em fronteira de fala (ver encaixar). Podem diferir
	// do que o operador marcou — por isso voltam, para ele ver onde caiu de fato.
	Start    string  `json:"start"`
	End      string  `json:"end"`
	StartSeg float64 `json:"start_seg"`
	EndSeg   float64 `json:"end_seg"`

	// Hook RECALCULADO: a primeira frase a partir do start. Não é opcional — ao estender
	// para trás, o hook deixa de ser a frase-âncora e passa a ser a abertura de fato.
	Hook          string  `json:"hook"`
	DuracaoSeg    float64 `json:"duration_seconds"`
	TextoFalado   string  `json:"texto_falado"`
	AjustadoStart bool    `json:"ajustado_start"` // o encaixe moveu o que o operador marcou?
	AjustadoEnd   bool    `json:"ajustado_end"`

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

// recalcularTrecho é o coração do ajuste: dados tempos novos (em segundos, como vêm do
// player), devolve o trecho recalculado — hook, duração e texto REALMENTE falado.
//
// O encaixe em fronteira de fala não é conveniência, é o que mantém verdadeira a invariante
// que o auditor verifica (spec-16: o hook começa EXATAMENTE no start, Δ=0). Sem encaixe, um
// start 0,4 s antes da frase faria o auditor acusar "sobra de abertura" em todo trecho
// ajustado à mão, e a saída seria ensinar uma exceção ao auditor — pior remédio que a
// doença. De quebra, dispensa precisão do operador: ele julga de ouvido, e a fronteira de
// fala é um evento de 0,1–0,3 s.
func recalcularTrecho(frases []harness.Frase, indice int, startSeg, endSeg float64, lim LimitesPregacao) TrechoAjustado {
	t := TrechoAjustado{Indice: indice}

	brutoIni := int(math.Round(startSeg * 1000))
	brutoFim := int(math.Round(endSeg * 1000))
	brutoIni, brutoFim = clampar(brutoIni, brutoFim, lim)

	if brutoFim <= brutoIni {
		t.StartSeg, t.EndSeg = float64(brutoIni)/1000, float64(brutoFim)/1000
		t.Start, t.End = hms(brutoIni), hms(brutoFim)
		t.Motivo = "o fim precisa vir depois do início"
		return t
	}

	iniIdx, okI := encaixarInicio(frases, brutoIni)
	fimMs, okF := encaixarFim(frases, brutoFim)
	if !okI || !okF {
		t.StartSeg, t.EndSeg = float64(brutoIni)/1000, float64(brutoFim)/1000
		t.Start, t.End = hms(brutoIni), hms(brutoFim)
		t.Motivo = "não há fala transcrita nessa faixa para ancorar o corte"
		return t
	}

	iniMs := frases[iniIdx].InicioMs
	t.AjustadoStart = iniMs != brutoIni
	t.AjustadoEnd = fimMs != brutoFim

	if fimMs <= iniMs {
		t.StartSeg, t.EndSeg = float64(iniMs)/1000, float64(fimMs)/1000
		t.Start, t.End = hms(iniMs), hms(fimMs)
		t.Motivo = "início e fim caíram na mesma fala; separe mais os dois pontos"
		return t
	}

	t.StartSeg, t.EndSeg = float64(iniMs)/1000, float64(fimMs)/1000
	t.Start, t.End = hms(iniMs), hms(fimMs)
	t.DuracaoSeg = float64(fimMs-iniMs) / 1000

	// Hook = a primeira frase real a partir do start final. MESMA regra da Fase 3
	// (fase3.go: "hook = a PRIMEIRA frase real a partir do start final"), reusada de
	// propósito: é o que faz a invariante do auditor valer por construção.
	t.Hook = frases[iniIdx].Texto
	t.TextoFalado = textoDoTrechoMs(frases, iniMs, fimMs)

	t.Aprovavel, t.Motivo = duracaoAceitavel(fimMs - iniMs)
	return t
}

// duracaoAceitavel aplica a faixa de CONSTRUÇÃO (a mesma da Fase 3 — harness.DuracaoMinMs/
// MaxMs, um lugar só) e explica em palavras o que falta, com os números. "Fora da faixa"
// não ajuda ninguém a consertar; "ficaria 64s, o máximo é 58s" ajuda.
func duracaoAceitavel(durMs int) (bool, string) {
	seg := float64(durMs) / 1000
	switch {
	case durMs < harness.DuracaoMinMs:
		falta := float64(harness.DuracaoMinMs-durMs) / 1000
		return false, fmt.Sprintf("ficaria %.1fs, o mínimo é %ds — estenda %.1fs",
			seg, harness.DuracaoMinMs/1000, falta)
	case durMs > harness.DuracaoMaxMs:
		sobra := float64(durMs-harness.DuracaoMaxMs) / 1000
		return false, fmt.Sprintf("ficaria %.1fs, o máximo é %ds — encurte %.1fs",
			seg, harness.DuracaoMaxMs/1000, sobra)
	}
	return true, ""
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

// encaixarInicio escolhe a frase cujo INÍCIO está mais perto do ponto marcado.
//
// Mais perto, e não "a próxima a partir de t": se o operador clicou 1 s depois de a frase
// começar, ele quer aquela frase (ouviu a abertura e reagiu). Se clicou pouco antes de a
// próxima começar, quer a próxima. A distância mínima captura as duas intenções sem
// precisar adivinhar direção.
func encaixarInicio(frases []harness.Frase, ms int) (int, bool) {
	melhor, dist := -1, math.MaxInt64
	for i, f := range frases {
		if d := abs(f.InicioMs - ms); d < dist {
			melhor, dist = i, d
		}
	}
	return melhor, melhor >= 0
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

// hms formata no MESMO formato da Fase 3 ("HH:MM:SS.000"). Não há perda: a transcrição
// limpa tem timestamps em segundos inteiros ([HH:MM:SS]), então toda fronteira de fala é
// múltiplo de 1000 ms. É também por isso que empurrão de ±0,25s no player não precisa de
// precisão: o encaixe absorve a diferença.
func hms(ms int) string { return validacao.MsParaHms(ms) + ".000" }

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
	c.DurationSeconds = t.DuracaoSeg
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

	var corpo struct {
		Indice int     `json:"indice"`
		Start  float64 `json:"start"`
		End    float64 `json:"end"`
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

	t := recalcularTrecho(frases, corpo.Indice, corpo.Start, corpo.End, lim)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(t)
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
		t := recalcularTrecho(frases, a.Indice, a.Start, a.End, lim)
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
