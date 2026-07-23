// Pacote validacao concentra a lógica determinística que confere e corrige os
// candidatos gerados pelo modelo contra a transcrição de origem. Não usa LLM.
//
// É a mesma lógica validada no spike (ver docs/aprendizados-do-spike.md) e antes
// embutida em cmd/validar. Foi extraída para cá (spec-02) para ser reusada tanto
// pelo comando `validar` quanto pela orquestração da seleção, sem duplicar regra.
//
// Correções aplicadas por Processar quando corrigir=true:
//   - start deslizado  -> reescreve com o horário real do hook
//   - hook inventado    -> descarta o candidato
//   - score != soma     -> recalcula pela soma dos critérios
//   - duration_seconds  -> recalcula a partir de end-start
package validacao

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const toleranciaMs = 2000 // start a mais que isso do hook = deslizado

var (
	reNaoAlnum = regexp.MustCompile(`[^a-z0-9 ]+`)
	reEspacos  = regexp.MustCompile(`\s+`)
	folder     = strings.NewReplacer(
		"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"í", "i", "ì", "i", "î", "i", "ï", "i",
		"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
		"ú", "u", "ù", "u", "û", "u", "ü", "u",
		"ç", "c", "ñ", "n",
	)
)

// Criteria são os cinco subcritérios pontuados pelo modelo. score = soma deles.
// Pesos (teto): fidelidade 30, valor pastoral 30, completude 20, abertura 10, formato 10.
type Criteria struct {
	ContextFidelity int `json:"context_fidelity"`
	PastoralValue   int `json:"pastoral_value"`
	Completeness    int `json:"completeness"`
	OpeningStrength int `json:"opening_strength"`
	FormatFit       int `json:"format_fit"`
}

// Soma devolve o total dos cinco critérios (o score correto).
func (c Criteria) Soma() int {
	return c.ContextFidelity + c.PastoralValue + c.Completeness + c.OpeningStrength + c.FormatFit
}

// Candidato é a forma tipada de um candidato já corrigido (usada pela orquestração).
// O comando validar continua trabalhando sobre o JSON cru para preservar campos
// desconhecidos e a formatação; esta forma tipada é para quem consome o resultado.
type Candidato struct {
	Start                  string   `json:"start"`
	End                    string   `json:"end"`
	DurationSeconds        float64  `json:"duration_seconds"`
	Score                  int      `json:"score"`
	Hook                   string   `json:"hook"`
	Reason                 string   `json:"reason,omitempty"`
	CompleteThought        bool     `json:"complete_thought"`
	RequerRevisaoReforcada bool     `json:"requer_revisao_reforcada"`
	MotivoRevisao          string   `json:"motivo_revisao,omitempty"`
	Criteria               Criteria `json:"criteria"`
}

// Palavra é uma palavra da transcrição com o horário (ms) do bloco em que aparece.
type Palavra struct {
	Txt string
	Ms  int
}

// ResultadoCandidato descreve o veredito de um candidato, sem formatação de saída.
type ResultadoCandidato struct {
	OK        bool
	Hook      string
	Problemas []string
	Acoes     []string
	Descartar bool
}

// Resultado agrega o veredito de todos os candidatos de um documento.
type Resultado struct {
	Candidatos []ResultadoCandidato
	// Mantidos são os candidatos preservados (crus), já com as correções aplicadas
	// quando Processar rodou com corrigir=true. Preserva campos desconhecidos.
	Mantidos []map[string]json.RawMessage
	// Total é a soma da quantidade de problemas encontrados em todos os candidatos.
	Total int
}

var obrigatorios = []string{"start", "end", "duration_seconds", "score", "hook", "complete_thought", "criteria"}

// Processar confere (e, se corrigir, corrige) os candidatos de doc contra palavras.
// Não imprime nada: devolve o veredito estruturado para quem chamou formatar/usar.
func Processar(doc map[string]json.RawMessage, palavras []Palavra, corrigir bool) (Resultado, error) {
	var res Resultado
	raw, ok := doc["candidatos"]
	if !ok {
		return res, fmt.Errorf("documento sem 'candidatos'")
	}
	var candidatos []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &candidatos); err != nil {
		return res, err
	}

	for _, cand := range candidatos {
		var probs []string
		var acoes []string
		descartar := false

		for _, campo := range obrigatorios {
			if _, ok := cand[campo]; !ok {
				probs = append(probs, fmt.Sprintf("falta o campo '%s'", campo))
			}
		}

		start := getStr(cand, "start")
		hook := getStr(cand, "hook")
		startMs, okStart := HmsToMs(start)
		endMs, okEnd := HmsToMs(getStr(cand, "end"))

		// --- Hook / start ---
		if okStart && hook != "" {
			hookMs, achou := AcharHook(palavras, hook, startMs)
			switch {
			case !achou:
				probs = append(probs, "hook não encontrado na transcrição")
				descartar = true
			case abs(hookMs-startMs) > 20000:
				probs = append(probs, fmt.Sprintf("hook aparece longe do start (em %s)", MsParaHms(hookMs)))
				descartar = true
			case abs(hookMs-startMs) > toleranciaMs:
				probs = append(probs, fmt.Sprintf("start deslizado: %s -> hook em %s", start, MsParaHms(hookMs)))
				if corrigir {
					setStr(cand, "start", MsParaHms(hookMs)+".000")
					startMs = hookMs
					acoes = append(acoes, "start corrigido para "+MsParaHms(hookMs))
				}
			}
		}

		// --- Tempos e duração (após possível correção do start) ---
		if okStart && okEnd && endMs <= startMs {
			probs = append(probs, "end não é maior que start")
			descartar = descartar || true
		}
		if okStart && okEnd && endMs > startMs {
			durReal := float64(endMs-startMs) / 1000.0
			if durReal > 60.5 {
				probs = append(probs, fmt.Sprintf("duração %.0fs passa de 60s", durReal))
			}
			if dur, ok := getFloat(cand, "duration_seconds"); ok && absF(dur-durReal) > 1.5 {
				probs = append(probs, fmt.Sprintf("duration_seconds=%.0f, real=%.0fs", dur, durReal))
			}
			if corrigir {
				setNum(cand, "duration_seconds", durReal)
			}
		}

		// --- Score = soma dos critérios ---
		if crRaw, ok := cand["criteria"]; ok {
			var cr Criteria
			if json.Unmarshal(crRaw, &cr) == nil {
				soma := cr.Soma()
				if sc, ok := getFloat(cand, "score"); ok && int(sc) != soma {
					probs = append(probs, fmt.Sprintf("score=%d, soma=%d", int(sc), soma))
				}
				if corrigir {
					setNum(cand, "score", float64(soma))
					acoes = append(acoes, fmt.Sprintf("score=%d (soma)", soma))
				}
			}
		}

		res.Candidatos = append(res.Candidatos, ResultadoCandidato{
			OK:        len(probs) == 0,
			Hook:      hook,
			Problemas: probs,
			Acoes:     acoes,
			Descartar: descartar,
		})
		res.Total += len(probs)

		if !descartar {
			res.Mantidos = append(res.Mantidos, cand)
		}
	}

	return res, nil
}

// LerTranscricao transforma "[HH:MM:SS] texto" em palavras normalizadas com o ms do bloco.
func LerTranscricao(raw string) []Palavra {
	var out []Palavra
	re := regexp.MustCompile(`^\[(\d{2}:\d{2}:\d{2})\]\s*(.*)$`)
	for _, linha := range strings.Split(raw, "\n") {
		m := re.FindStringSubmatch(strings.TrimSpace(linha))
		if m == nil {
			continue
		}
		ms, ok := HmsToMs(m[1])
		if !ok {
			continue
		}
		for _, w := range strings.Fields(Normalizar(m[2])) {
			out = append(out, Palavra{Txt: w, Ms: ms})
		}
	}
	return out
}

// AcharHook procura as primeiras palavras do hook na transcrição e devolve o ms do
// bloco onde melhor casa (o mais próximo de startMs). Retorna false se não achar.
func AcharHook(palavras []Palavra, hook string, startMs int) (int, bool) {
	alvo := strings.Fields(Normalizar(hook))
	if len(alvo) == 0 {
		return 0, false
	}
	k := 5
	if len(alvo) < k {
		k = len(alvo)
	}
	alvo = alvo[:k]
	melhorMs, achou := 0, false
	for i := 0; i+k <= len(palavras); i++ {
		match := true
		for j := 0; j < k; j++ {
			if palavras[i+j].Txt != alvo[j] {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		ms := palavras[i].Ms
		if !achou || abs(ms-startMs) < abs(melhorMs-startMs) {
			melhorMs, achou = ms, true
		}
	}
	return melhorMs, achou
}

// Normalizar baixa caixa, remove acentos e pontuação e colapsa espaços.
func Normalizar(s string) string {
	s = strings.ToLower(s)
	s = folder.Replace(s)
	s = reNaoAlnum.ReplaceAllString(s, " ")
	return strings.TrimSpace(reEspacos.ReplaceAllString(s, " "))
}

// HmsToMs converte "HH:MM:SS", "HH:MM:SS,mmm" ou "HH:MM:SS.mmm" em milissegundos.
func HmsToMs(s string) (int, bool) {
	s = strings.TrimSpace(s)
	ms := 0
	if i := strings.IndexAny(s, ",."); i != -1 {
		frac := s[i+1:]
		s = s[:i]
		for len(frac) < 3 {
			frac += "0"
		}
		var f int
		if _, err := fmt.Sscanf(frac[:3], "%d", &f); err == nil {
			ms = f
		}
	}
	var h, m, sec int
	if _, err := fmt.Sscanf(s, "%d:%d:%d", &h, &m, &sec); err != nil {
		return 0, false
	}
	return ((h*60+m)*60+sec)*1000 + ms, true
}

// MsParaHms devolve "HH:MM:SS" (sem milissegundos).
func MsParaHms(ms int) string {
	t := ms / 1000
	return fmt.Sprintf("%02d:%02d:%02d", t/3600, (t%3600)/60, t%60)
}

func getStr(m map[string]json.RawMessage, k string) string {
	if raw, ok := m[k]; ok {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
	}
	return ""
}

func getFloat(m map[string]json.RawMessage, k string) (float64, bool) {
	if raw, ok := m[k]; ok {
		var f float64
		if json.Unmarshal(raw, &f) == nil {
			return f, true
		}
	}
	return 0, false
}

func setStr(m map[string]json.RawMessage, k, v string) {
	b, _ := json.Marshal(v)
	m[k] = b
}

func setNum(m map[string]json.RawMessage, k string, v float64) {
	b, _ := json.Marshal(v)
	m[k] = b
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func absF(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
