package servidor

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"srtclean/internal/pipeline"
	"srtclean/internal/validacao"
)

// registrarRodada anexa ao log em disco (LogRodadasPath) uma "rodada": o resultado de
// uma seleção, para avaliar variância entre execuções/sermões. Cada rodada traz o
// contexto do pedido e os candidatos ORDENADOS por score (desc), em tabela markdown.
//
// É auxiliar: qualquer falha de I/O só emite um aviso no stderr, nunca interrompe a
// seleção. Serializa a escrita com logMu (a fase leve roda em goroutine).
func (s *Servidor) registrarRodada(ped *pipeline.Pedido, cands []validacao.Candidato) {
	if s.logRodadasPath == "" {
		return
	}
	s.logMu.Lock()
	defer s.logMu.Unlock()

	if dir := filepath.Dir(s.logRodadasPath); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "aviso: não criei o diretório do log de rodadas: %v\n", err)
			return
		}
	}
	n := proximaRodada(s.logRodadasPath)

	// Ordena uma CÓPIA por score desc — a ordem original de `cands` é a que os índices
	// de aprovação usam, então não pode ser mexida.
	ord := make([]validacao.Candidato, len(cands))
	copy(ord, cands)
	sort.SliceStable(ord, func(i, j int) bool { return ord[i].Score > ord[j].Score })

	texto := formatarRodada(n, s.agora(), ped, ord)

	f, err := os.OpenFile(s.logRodadasPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aviso: não abri o log de rodadas: %v\n", err)
		return
	}
	defer f.Close()
	if _, err := f.WriteString(texto); err != nil {
		fmt.Fprintf(os.Stderr, "aviso: não escrevi no log de rodadas: %v\n", err)
	}
}

// formatarRodada monta o bloco markdown de uma rodada: cabeçalho com o contexto do
// pedido e uma tabela dos candidatos JÁ ordenados por score.
func formatarRodada(n int, agora time.Time, ped *pipeline.Pedido, ord []validacao.Candidato) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Rodada %d — %s\n\n", n, agora.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "- Pedido: %s\n", ped.ID)
	if ped.Titulo != "" {
		fmt.Fprintf(&b, "- Título: %s\n", celula(ped.Titulo))
	}
	fmt.Fprintf(&b, "- Vídeo: %s\n", ped.YouTubeURL)
	fmt.Fprintf(&b, "- Janela: %s → %s\n", ped.Inicio, ped.Fim)
	fmt.Fprintf(&b, "- Candidatos: %d\n\n", len(ord))

	b.WriteString("| # | score | início → fim | dur | revisão | hook |\n")
	b.WriteString("|---|-------|--------------|-----|---------|------|\n")
	for i, c := range ord {
		rev := "—"
		if c.RequerRevisaoReforcada {
			rev = "⚠ " + c.MotivoRevisao
		}
		fmt.Fprintf(&b, "| %d | %d | %s → %s | %.0fs | %s | %s |\n",
			i+1, c.Score, c.Start, c.End, c.DurationSeconds, celula(rev), celula(c.Hook))
	}
	b.WriteString("\n")
	return b.String()
}

// proximaRodada devolve o número da próxima rodada, contando os cabeçalhos "## Rodada "
// já presentes no arquivo (1 se o arquivo não existe). Assim a numeração é contínua
// mesmo entre reinícios do servidor.
func proximaRodada(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 1
	}
	n := 0
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "## Rodada ") {
			n++
		}
	}
	return n + 1
}

// celula deixa um texto seguro para uma célula de tabela markdown (sem quebras nem "|").
func celula(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "/")
	return strings.TrimSpace(s)
}
