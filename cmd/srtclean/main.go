// Comando srtclean: converte uma legenda .srt (ex.: autocaption do YouTube)
// em uma transcrição enxuta, uma fala por linha no formato "[HH:MM:SS] texto".
//
// É 100% determinístico: não usa LLM e NÃO altera nenhuma palavra do pregador.
// A lógica de limpeza mora em internal/transcricao (spec-03); este comando é uma
// camada fina de linha de comando sobre ela. O comportamento é idêntico ao da spec-01.
//
// Uso:
//
//	go run . -in sermao.srt -out sermao.txt
//	go build -o srtclean . && ./srtclean -in sermao.srt -out sermao.txt
package main

import (
	"flag"
	"fmt"
	"os"

	"srtclean/internal/transcricao"
)

func main() {
	in := flag.String("in", "", "arquivo .srt de entrada (obrigatório)")
	out := flag.String("out", "", "arquivo de saída (obrigatório)")
	until := flag.String("until", "", "opcional: descarta falas a partir deste tempo (ex.: 00:33:10)")
	from := flag.String("from", "", "opcional: descarta falas antes deste tempo (ex.: 01:29:38)")
	flag.Parse()

	if *in == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "uso: srtclean -in entrada.srt -out saida.txt [-from 01:29:38] [-until 02:05:11]")
		flag.PrintDefaults()
		os.Exit(2)
	}

	untilMs := -1
	if *until != "" {
		v, ok := hmsToMs(*until)
		if !ok {
			fmt.Fprintf(os.Stderr, "-until inválido: %q (use HH:MM:SS)\n", *until)
			os.Exit(2)
		}
		untilMs = v
	}
	fromMs := -1
	if *from != "" {
		v, ok := hmsToMs(*from)
		if !ok {
			fmt.Fprintf(os.Stderr, "-from inválido: %q (use HH:MM:SS)\n", *from)
			os.Exit(2)
		}
		fromMs = v
	}

	blocks, linhas, err := transcricao.LimparArquivoJanela(*in, *out, fromMs, untilMs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro ao processar %q -> %q: %v\n", *in, *out, err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "ok: %d blocos lidos, %d linhas escritas em %s\n",
		blocks, linhas, *out)
}

// --- Auxiliares expostos para os testes (delegam ao pacote internal/transcricao) ---

func clean(raw string, untilMs int) (lines []string, blocks int) {
	return transcricao.Limpar(raw, untilMs)
}

func cleanText(t string) string { return transcricao.CleanText(t) }

func hmsToMs(s string) (int, bool) { return transcricao.HmsToMs(s) }

func formatMs(ms int) string { return transcricao.FormatMs(ms) }
