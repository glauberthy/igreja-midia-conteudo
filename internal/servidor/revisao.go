package servidor

import (
	"encoding/json"
	"html/template"
	"os"
	"sort"
	"strings"

	"srtclean/internal/harness"
	"srtclean/internal/transcricao"
	"srtclean/internal/validacao"
)

// textosFalados devolve, para cada candidato, o texto REALMENTE falado na janela
// [start, end] — reconstruído da transcrição via harness.Frasear (a mesma verdade
// textual da Fase 3 e do cmd/auditar). É o artefato principal da revisão (spec-05, tela
// de revisão): o operador lê isto para julgar doutrina. Best-effort: se a transcrição
// não abrir, devolve textos vazios (a UI cai para o hook).
func textosFalados(transcPath string, cands []validacao.Candidato) []string {
	out := make([]string, len(cands))
	b, err := os.ReadFile(transcPath)
	if err != nil {
		return out
	}
	frases := harness.Frasear(string(b))
	for i, c := range cands {
		out[i] = textoDoTrecho(frases, c.Start, c.End)
	}
	return out
}

// textoDoTrecho junta as frases cujo início cai em [start, end).
func textoDoTrecho(frases []harness.Frase, start, end string) string {
	ini, okI := transcricao.HmsToMs(start)
	fim, okF := transcricao.HmsToMs(end)
	if !okI || !okF {
		return ""
	}
	return textoDoTrechoMs(frases, ini, fim)
}

// textoDoTrechoMs é a mesma junção, em ms — usada pelo ajuste manual, que trabalha com os
// tempos do player. Uma regra só para os dois caminhos: o texto que o operador lê na
// revisão tem de ser o mesmo que o ajuste recalcula, senão ele julga uma coisa e aprova
// outra.
func textoDoTrechoMs(frases []harness.Frase, ini, fim int) string {
	var partes []string
	for _, f := range frases {
		if f.InicioMs >= ini && f.InicioMs < fim {
			partes = append(partes, f.Texto)
		}
	}
	return strings.Join(partes, " ")
}

// trechoRevisao é o dado de um trecho que o front (JS) consome para desenhar a tela de
// revisão. Tempos em segundos (startSeg/endSeg) para o seekTo do player.
//
// Indice é o índice ORIGINAL do candidato (o que o POST /aprovar espera) — preservado
// mesmo depois de reordenar os trechos para exibição.
type trechoRevisao struct {
	Indice      int    `json:"indice"`
	Texto       string `json:"texto"`  // o que foi falado (artefato de revisão)
	Hook        string `json:"hook"`   // fallback quando texto vazio
	Inicio      string `json:"inicio"` // HH:MM:SS
	Fim         string `json:"fim"`
	StartSeg    int    `json:"startSeg"`
	EndSeg      int    `json:"endSeg"`
	Dur         int    `json:"dur"`
	Score       int    `json:"score"`
	MelhorScore bool   `json:"melhorScore"` // selo discreto "maior score" (destaque sem reordenar)
	Revisar     bool   `json:"revisar"`
	Motivo      string `json:"motivo"`
}

// dadosRevisao é o payload JSON embutido na página de revisão.
type dadosRevisao struct {
	PedidoID string          `json:"pedidoId"`
	VideoID  string          `json:"videoId"`
	Trechos  []trechoRevisao `json:"trechos"`
}

// revisaoJSON monta o JSON (seguro para <script>) com os dados de revisão do registro.
// Chamar com o lock do servidor seguro (lê reg).
func revisaoJSON(reg *registro) template.JS {
	d := dadosRevisao{
		PedidoID: reg.ped.ID,
		VideoID:  videoID(reg.ped.YouTubeURL),
	}
	// Índice do candidato de MAIOR score — para o selo discreto (destaca sem reordenar).
	melhor := -1
	for i, c := range reg.cands {
		if melhor == -1 || c.Score > reg.cands[melhor].Score {
			melhor = i
		}
	}
	for i, c := range reg.cands {
		ini, _ := transcricao.HmsToMs(c.Start)
		fim, _ := transcricao.HmsToMs(c.End)
		texto := ""
		if i < len(reg.textos) {
			texto = reg.textos[i]
		}
		d.Trechos = append(d.Trechos, trechoRevisao{
			Indice:      i, // índice ORIGINAL (o /aprovar usa este), preservado ao reordenar
			Texto:       texto,
			Hook:        c.Hook,
			Inicio:      cortaHms(c.Start),
			Fim:         cortaHms(c.End),
			StartSeg:    ini / 1000,
			EndSeg:      fim / 1000,
			Dur:         int(c.DurationSeconds + 0.5),
			Score:       c.Score,
			MelhorScore: i == melhor,
			Revisar:     c.RequerRevisaoReforcada,
			Motivo:      c.MotivoRevisao,
		})
	}
	// Ordem CRONOLÓGICA na revisão (ordem do sermão), NÃO por score — decisão da spec-05
	// ("Ordenação: revisão cronológica, render por score"): ordenar por score empurraria os
	// trechos marcados (fidelidade baixa derruba o score) para o fim, justo os que mais
	// precisam do olho do pastor. O índice original é preservado para o /aprovar.
	sort.SliceStable(d.Trechos, func(a, b int) bool { return d.Trechos[a].StartSeg < d.Trechos[b].StartSeg })
	// json.Marshal escapa < > & (<…), então o resultado é seguro dentro de <script>.
	b, err := json.Marshal(d)
	if err != nil {
		return template.JS(`{"trechos":[]}`)
	}
	return template.JS(b)
}

// cortaHms devolve "HH:MM:SS" (sem os milissegundos ".000") para exibição.
func cortaHms(s string) string {
	if i := strings.IndexByte(s, '.'); i != -1 {
		return s[:i]
	}
	return s
}
