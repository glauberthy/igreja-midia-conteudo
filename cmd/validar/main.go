// Comando validar: confere (e opcionalmente corrige) os candidatos gerados
// pelo modelo contra a transcrição de origem. Determinístico, sem LLM.
//
// A lógica de conferência/correção mora em internal/validacao (spec-02); este
// comando é uma camada fina que lê os arquivos, formata o relato e grava o
// .corrigido.json. O comportamento é idêntico ao da spec-01.
//
// Modo padrão (detecta e reporta):
//
//	go run . -de 1 -ate 5
//
// Modo corretor (-corrigir): grava resultados/candidatos_N.corrigido.json
//   - start deslizado  -> reescreve com o horário real do hook
//   - hook inventado    -> descarta o candidato
//   - score != soma     -> recalcula pela soma dos critérios
//   - duration_seconds  -> recalcula a partir de end-start
//     go run . -de 1 -ate 5 -corrigir
//
// Um par único:
//
//	go run . -json resultados/candidatos_2.json -transc transcricao_2.txt -corrigir
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"srtclean/internal/validacao"
)

// palavra é a forma que os testes da spec-01 esperam de lerTranscricao.
type palavra struct {
	txt string
	ms  int
}

var corrigir bool

func main() {
	de := flag.Int("de", 1, "número inicial (modo lote)")
	ate := flag.Int("ate", 5, "número final (modo lote)")
	dir := flag.String("dir", "resultados", "pasta dos candidatos_N.json")
	prefixo := flag.String("transc", "transcricao_", "prefixo das transcrições (ou caminho único com -json)")
	jsonUnico := flag.String("json", "", "caminho de um único candidatos.json")
	flag.BoolVar(&corrigir, "corrigir", false, "grava versão corrigida (.corrigido.json)")
	flag.Parse()

	totalProblemas := 0
	if *jsonUnico != "" {
		totalProblemas += validarPar(*jsonUnico, *prefixo, 0)
	} else {
		for i := *de; i <= *ate; i++ {
			jsonPath := fmt.Sprintf("%s/candidatos_%d.json", *dir, i)
			transcPath := fmt.Sprintf("%s%d.txt", *prefixo, i)
			if _, err := os.Stat(jsonPath); err != nil {
				continue
			}
			totalProblemas += validarPar(jsonPath, transcPath, i)
		}
	}

	fmt.Println()
	if corrigir {
		fmt.Println("Correção concluída. Arquivos .corrigido.json gravados.")
	} else if totalProblemas == 0 {
		fmt.Println("Tudo certo: nenhum problema encontrado.")
	} else {
		fmt.Printf("Total de %d problema(s) encontrado(s). Rode com -corrigir para gerar a versão corrigida.\n", totalProblemas)
		os.Exit(1)
	}
}

func validarPar(jsonPath, transcPath string, n int) int {
	rotulo := jsonPath
	if n > 0 {
		rotulo = fmt.Sprintf("Sermão %d", n)
	}
	fmt.Printf("=== %s ===\n", rotulo)

	transRaw, err := os.ReadFile(transcPath)
	if err != nil {
		fmt.Printf("  ERRO: não abri a transcrição %q: %v\n", transcPath, err)
		return 1
	}
	palavras := validacao.LerTranscricao(string(transRaw))

	jsonRaw, err := os.ReadFile(jsonPath)
	if err != nil {
		fmt.Printf("  ERRO: não abri %q: %v\n", jsonPath, err)
		return 1
	}

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(jsonRaw, &doc); err != nil {
		fmt.Printf("  ERRO: JSON inválido: %v\n", err)
		return 1
	}

	res, err := validacao.Processar(doc, palavras, corrigir)
	if err != nil {
		fmt.Printf("  ERRO: não li 'candidatos': %v\n", err)
		return 1
	}

	for idx, rc := range res.Candidatos {
		if rc.OK {
			fmt.Printf("  candidato %d: OK\n", idx+1)
			continue
		}
		fmt.Printf("  candidato %d (%s):\n", idx+1, resumo(rc.Hook))
		for _, p := range rc.Problemas {
			fmt.Printf("      - %s\n", p)
		}
		if corrigir {
			if rc.Descartar {
				fmt.Printf("      => DESCARTADO\n")
			} else {
				for _, a := range rc.Acoes {
					fmt.Printf("      => %s\n", a)
				}
			}
		}
	}

	if corrigir {
		novos, _ := json.Marshal(res.Mantidos)
		doc["candidatos"] = novos
		saida, _ := json.MarshalIndent(doc, "", "  ")
		out := strings.TrimSuffix(jsonPath, ".json") + ".corrigido.json"
		if err := os.WriteFile(out, saida, 0644); err != nil {
			fmt.Printf("  ERRO ao gravar %q: %v\n", out, err)
		} else {
			fmt.Printf("  -> %d candidato(s) mantido(s), gravado em %s\n", len(res.Mantidos), out)
		}
	}
	return res.Total
}

// --- Auxiliares expostos para os testes (delegam ao pacote internal/validacao) ---

func normalizar(s string) string { return validacao.Normalizar(s) }

func lerTranscricao(raw string) []palavra {
	ws := validacao.LerTranscricao(raw)
	out := make([]palavra, len(ws))
	for i, w := range ws {
		out[i] = palavra{txt: w.Txt, ms: w.Ms}
	}
	return out
}

func acharHook(palavras []palavra, hook string, startMs int) (int, bool) {
	ws := make([]validacao.Palavra, len(palavras))
	for i, p := range palavras {
		ws[i] = validacao.Palavra{Txt: p.txt, Ms: p.ms}
	}
	return validacao.AcharHook(ws, hook, startMs)
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

func resumo(s string) string {
	if len(s) > 50 {
		return s[:50] + "..."
	}
	return s
}
