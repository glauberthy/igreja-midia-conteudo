package servidor

import (
	"encoding/json"
	"html/template"
	"os"
	"path/filepath"

	"srtclean/internal/download"
	"srtclean/internal/pipeline"
	"srtclean/internal/transcricao"
	"srtclean/internal/validacao"
)

// visaoStatus é o que a rota GET /pedidos/{id} apresenta — em HTML (template "status")
// ou em JSON. Mantém uma única fonte para os dois formatos.
type visaoStatus struct {
	ID           string
	Status       pipeline.Estado
	StatusLabel  string
	EmProgresso  bool
	Erro         string
	VideoID      string      // extraído da URL do pedido, para o player YouTube (spec-05 parte 2)
	RevisaoDados template.JS // JSON dos trechos p/ a tela de revisão (só em aguardando-aprovacao)
	Candidatos   []candidatoVis
	// Índices aprovados (contrato JSON de GET /pedidos/{id}).
	Aprovados []candidatoVis
	// Fase pesada concluída (spec-05 parte 3): os Shorts gerados, para download.
	Concluido bool
	Shorts    []string
}

// candidatoVis é a forma exibida de um candidato (apenas o que a revisão precisa).
// StartSeg/EndSeg são o start/end em SEGUNDOS ABSOLUTOS do vídeo original — o player
// do YouTube toca o vídeo inteiro, então o seekTo usa o tempo absoluto (o mesmo do corte).
type candidatoVis struct {
	Indice                 int
	Hook                   string
	Start                  string
	End                    string
	StartSeg               int
	EndSeg                 int
	DurationSeconds        float64
	Score                  int
	RequerRevisaoReforcada bool
	MotivoRevisao          string
}

// rotulosEtapa dá o texto amigável de cada estado de progresso.
var rotulosEtapa = map[pipeline.Estado]string{
	pipeline.EstadoBaixandoLegenda:         "baixando legenda",
	pipeline.EstadoSelecionando:            "selecionando trechos",
	pipeline.EstadoValidando:               "validando",
	pipeline.EstadoAguardandoProcessamento: "preparando…",
	pipeline.EstadoBaixandoVideo:           "baixando o vídeo dos aprovados",
	pipeline.EstadoRenderizando:            "renderizando os Shorts",
}

// emProgresso são os estados (fase leve E fase pesada) em que o polling deve continuar.
func emProgresso(e pipeline.Estado) bool {
	switch e {
	case pipeline.EstadoBaixandoLegenda, pipeline.EstadoSelecionando, pipeline.EstadoValidando,
		pipeline.EstadoAguardandoProcessamento, pipeline.EstadoBaixandoVideo, pipeline.EstadoRenderizando:
		return true
	}
	return false
}

// montarVisao converte o registro em uma visão para HTML/JSON. Deve ser chamada com
// o lock do servidor seguro (lê campos do registro).
func montarVisao(reg *registro) visaoStatus {
	v := visaoStatus{
		ID:          reg.ped.ID,
		Status:      reg.ped.Status,
		StatusLabel: rotulosEtapa[reg.ped.Status],
		EmProgresso: emProgresso(reg.ped.Status),
		Erro:        reg.ped.Erro,
		VideoID:     download.VideoID(reg.ped.YouTubeURL),
		Concluido:   reg.ped.Status == pipeline.EstadoConcluido,
		Shorts:      append([]string(nil), reg.shorts...),
	}
	if v.StatusLabel == "" {
		v.StatusLabel = string(reg.ped.Status)
	}
	for i, c := range reg.cands {
		v.Candidatos = append(v.Candidatos, candidatoParaVis(i, c))
	}
	// Dados da tela de revisão (só quando há candidatos para revisar).
	if reg.ped.Status == pipeline.EstadoAguardandoAprovacao && len(reg.cands) > 0 {
		v.RevisaoDados = revisaoJSON(reg)
	}
	for _, idx := range reg.aprovados {
		if idx >= 0 && idx < len(reg.cands) {
			v.Aprovados = append(v.Aprovados, candidatoParaVis(idx, reg.cands[idx]))
		}
	}
	return v
}

// candidatoParaVis converte um Candidato validado na forma exibida, calculando os
// tempos absolutos em segundos para o player YouTube.
func candidatoParaVis(indice int, c validacao.Candidato) candidatoVis {
	ini, _ := transcricao.HmsToMs(c.Start)
	fim, _ := transcricao.HmsToMs(c.End)
	return candidatoVis{
		Indice:                 indice,
		Hook:                   c.Hook,
		Start:                  c.Start,
		End:                    c.End,
		StartSeg:               ini / 1000,
		EndSeg:                 fim / 1000,
		DurationSeconds:        c.DurationSeconds,
		Score:                  c.Score,
		RequerRevisaoReforcada: c.RequerRevisaoReforcada,
		MotivoRevisao:          c.MotivoRevisao,
	}
}

// statusJSON e candidatoJSON são o contrato JSON de GET /pedidos/{id} (spec-05).
type statusJSON struct {
	ID         string          `json:"id"`
	Status     string          `json:"status"`
	Erro       string          `json:"erro,omitempty"`
	Candidatos []candidatoJSON `json:"candidatos,omitempty"`
	Aprovados  []int           `json:"aprovados,omitempty"`
	// Shorts: o cliente monta a tela de resultado com estes nomes. Antes o servidor mandava a
	// lista já em HTML; com as quatro telas no DOM, ele manda o DADO (spec-05 v3).
	Shorts []string `json:"shorts,omitempty"`
}

type candidatoJSON struct {
	Indice                 int     `json:"indice"`
	Hook                   string  `json:"hook"`
	Start                  string  `json:"start"`
	End                    string  `json:"end"`
	DurationSeconds        float64 `json:"duration_seconds"`
	Score                  int     `json:"score"`
	RequerRevisaoReforcada bool    `json:"requer_revisao_reforcada"`
	MotivoRevisao          string  `json:"motivo_revisao,omitempty"`
}

// EstadoJSON é o mesmo contrato JSON, embutido no fragmento HTML que o HTMX troca. O cliente
// lê daqui e distribui pelas quatro telas (spec-05 v3) — antes o servidor mandava a tela de
// revisão montada, e era isso que amarrava navegação a requisição.
//
// Uma fonte só para os dois consumidores (a API JSON e o fragmento), pelo mesmo motivo de
// sempre: dois formatos derivados do mesmo montador não divergem.
func (v visaoStatus) EstadoJSON() template.JS {
	b, err := json.Marshal(v.json())
	if err != nil {
		return template.JS(`{"status":"erro","erro":"falha ao serializar o estado"}`)
	}
	return template.JS(b)
}

func (v visaoStatus) json() statusJSON {
	out := statusJSON{ID: v.ID, Status: string(v.Status), Erro: v.Erro, Shorts: v.Shorts}
	for _, c := range v.Aprovados {
		out.Aprovados = append(out.Aprovados, c.Indice)
	}
	for _, c := range v.Candidatos {
		out.Candidatos = append(out.Candidatos, candidatoJSON{
			Indice:                 c.Indice,
			Hook:                   c.Hook,
			Start:                  c.Start,
			End:                    c.End,
			DurationSeconds:        c.DurationSeconds,
			Score:                  c.Score,
			RequerRevisaoReforcada: c.RequerRevisaoReforcada,
			MotivoRevisao:          c.MotivoRevisao,
		})
	}
	return out
}

// salvarCandidatos grava os candidatos validados em <base>/<id>/candidatos.corrigido.json,
// a fonte única de verdade lida pela fase pesada/cmd/render (spec-09).
func salvarCandidatos(base, id string, cands []validacao.Candidato) error {
	dir := filepath.Join(base, id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	doc := struct {
		Candidatos []validacao.Candidato `json:"candidatos"`
	}{Candidatos: cands}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "candidatos.corrigido.json"), b, 0644)
}
