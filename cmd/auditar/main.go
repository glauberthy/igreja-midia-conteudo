// Comando auditar: cruza os candidatos VALIDADOS de um pedido com a legenda real
// (transcrição desduplicada) e reporta, por trecho, as invariantes de fidelidade:
//
//   - o hook existe na legenda e começa EXATAMENTE no start (Δ=0)? Um Δ negativo
//     significa hook clipado (o corte começa depois do início da frase de abertura);
//   - o end cai no fim de uma frase COMPLETA (não corta fala no meio)?
//   - a duração está em 30–60 s?
//
// Também imprime (com -texto) o texto realmente falado dentro da janela [start, end],
// para a revisão humana de teologia — a auditoria confere o texto; quem julga doutrina
// é o pastor (regra inviolável nº 6).
//
// Uso:
//
//	go run ./cmd/auditar -id sermao                    # um pedido
//	go run ./cmd/auditar -todos                        # todos em trabalho/
//	go run ./cmd/auditar -id sermao -texto             # inclui o texto falado
//
// Saída em markdown no stdout (redirecione para um arquivo se quiser guardar).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"srtclean/internal/harness"
	"srtclean/internal/validacao"
)

func main() {
	id := flag.String("id", "", "identificador do pedido a auditar")
	base := flag.String("base", "trabalho", "pasta raiz de trabalho")
	todos := flag.Bool("todos", false, "audita todos os pedidos com candidatos em <base>/")
	comTexto := flag.Bool("texto", false, "inclui o texto falado de cada trecho (para revisão teológica)")
	flag.Parse()

	if *id == "" && !*todos {
		fmt.Fprintln(os.Stderr, "uso: auditar -id ID | -todos  [-base trabalho] [-texto]")
		flag.PrintDefaults()
		os.Exit(2)
	}

	ids := []string{*id}
	if *todos {
		ids = pedidosComCandidatos(*base)
		if len(ids) == 0 {
			fmt.Fprintln(os.Stderr, "nenhum pedido com candidatos.corrigido.json em", *base)
			os.Exit(1)
		}
	}

	problemasTotais := 0
	for _, pid := range ids {
		n, err := auditarPedido(os.Stdout, *base, pid, *comTexto)
		if err != nil {
			fmt.Fprintf(os.Stderr, "erro em %s: %v\n", pid, err)
			continue
		}
		problemasTotais += n
	}
	if problemasTotais > 0 {
		fmt.Printf("\n**Total: %d problema(s) encontrado(s).**\n", problemasTotais)
		os.Exit(1)
	}
	fmt.Println("\n**Nenhum problema de fidelidade encontrado.**")
}

// pedidosComCandidatos lista (ordenado) os ids em base/ que têm candidatos validados.
func pedidosComCandidatos(base string) []string {
	matches, _ := filepath.Glob(filepath.Join(base, "*", "candidatos.corrigido.json"))
	var ids []string
	for _, m := range matches {
		ids = append(ids, filepath.Base(filepath.Dir(m)))
	}
	sort.Strings(ids)
	return ids
}

// auditarPedido audita um pedido e escreve o relatório em w. Devolve o nº de problemas.
func auditarPedido(w *os.File, base, id string, comTexto bool) (int, error) {
	dir := filepath.Join(base, id)
	trBytes, err := os.ReadFile(filepath.Join(dir, "transcricao.txt"))
	if err != nil {
		return 0, fmt.Errorf("sem transcrição: %w", err)
	}
	candBytes, err := os.ReadFile(filepath.Join(dir, "candidatos.corrigido.json"))
	if err != nil {
		return 0, fmt.Errorf("sem candidatos validados: %w", err)
	}
	var doc struct {
		Candidatos []validacao.Candidato `json:"candidatos"`
	}
	if err := json.Unmarshal(candBytes, &doc); err != nil {
		return 0, fmt.Errorf("candidatos ilegíveis: %w", err)
	}

	frases := harness.Frasear(string(trBytes))
	fmt.Fprintf(w, "## %s — %d candidato(s)\n\n", id, len(doc.Candidatos))

	problemas := 0
	for i, c := range doc.Candidatos {
		probs, texto := AuditarCandidato(frases, c)
		status := "✅ fiel"
		if len(probs) > 0 {
			status = "⚠ " + strings.Join(probs, "; ")
			problemas += len(probs)
		}
		fmt.Fprintf(w, "%d. [%s → %s] score %d — %s\n   hook: %s\n", i+1, c.Start, c.End, c.Score, status, c.Hook)
		if comTexto {
			fmt.Fprintf(w, "   falado: %s\n", texto)
		}
	}
	fmt.Fprintln(w)
	return problemas, nil
}

// AuditarCandidato confere as invariantes de um candidato contra as frases da legenda.
// Devolve os problemas encontrados (vazio = fiel) e o texto falado da janela.
func AuditarCandidato(frases []harness.Frase, c validacao.Candidato) (probs []string, texto string) {
	startMs, okS := validacao.HmsToMs(c.Start)
	endMs, okE := validacao.HmsToMs(c.End)
	if !okS || !okE || endMs <= startMs {
		return []string{"tempos ilegíveis ou end<=start"}, ""
	}

	// 1) Hook existe na legenda e começa no start?
	if idx, achou := harness.AcharAncora(frases, c.Hook); !achou {
		probs = append(probs, "hook não encontrado na legenda (inventado?)")
	} else if delta := frases[idx].InicioMs - startMs; delta < 0 {
		probs = append(probs, fmt.Sprintf("hook CLIPADO: começa %.0fs antes do start", float64(-delta)/1000))
	} else if delta > 0 {
		probs = append(probs, fmt.Sprintf("start %.0fs antes do hook (sobra de abertura)", float64(delta)/1000))
	}

	// 2) O end cai no fim de uma frase completa?
	fimOK := false
	for _, f := range frases {
		if f.FimMs == endMs && f.Completa {
			fimOK = true
			break
		}
	}
	if !fimOK {
		probs = append(probs, "end não coincide com fim de frase completa (pode cortar fala)")
	}

	// 3) Duração na faixa do produto.
	if dur := float64(endMs-startMs) / 1000; dur < 30 || dur > 60 {
		probs = append(probs, fmt.Sprintf("duração fora de 30–60s (%.0fs)", dur))
	}

	// Texto realmente falado na janela (para a revisão humana).
	var partes []string
	for _, f := range frases {
		if f.InicioMs >= startMs && f.InicioMs < endMs {
			partes = append(partes, f.Texto)
		}
	}
	return probs, strings.Join(partes, " ")
}
