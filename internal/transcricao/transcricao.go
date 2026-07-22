// Pacote transcricao concentra a limpeza determinística de legendas .srt (ou .vtt)
// para o formato de transcrição "[HH:MM:SS] texto", uma fala por linha.
//
// É a mesma lógica validada no spike e antes embutida em cmd/srtclean. Foi extraída
// para cá (spec-03) para ser reusada pelo comando `srtclean` e pelo download.
//
// NÃO altera nenhuma palavra do pregador (BRD RN-013): só descarta numeração e as
// setas do SRT, remove marcação (tags <...> e {...}), anotações ([Música]), normaliza
// espaços e usa o tempo de INÍCIO de cada bloco. Inícios são forçados a não retroceder,
// o que resolve a sobreposição de tempos do autocaption.
package transcricao

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	reTagAngle   = regexp.MustCompile(`<[^>]*>`)    // <i>, <c>, tags de posição, etc.
	reTagBrace   = regexp.MustCompile(`\{[^}]*\}`)  // {\an8} e afins
	reAnnotation = regexp.MustCompile(`\[[^\]]*\]`) // [Música], [Aplausos], [Risadas]
	reSpaces     = regexp.MustCompile(`\s+`)        // colapsa espaços/quebras
	reBlocos     = regexp.MustCompile(`\n[ \t]*\n`) // separador de blocos
)

// Limpar transforma o conteúdo bruto do SRT nas linhas de saída.
// untilMs >= 0 descarta falas cujo início seja igual ou posterior a esse tempo.
// Retorna também quantos blocos foram lidos (para o resumo).
func Limpar(raw string, untilMs int) (lines []string, blocks int) {
	return LimparJanela(raw, -1, untilMs)
}

// LimparJanela \u00e9 como Limpar, mas restringe \u00e0 janela [fromMs, untilMs) \u2014 mantendo
// os tempos ABSOLUTOS de cada fala. fromMs < 0 = sem limite inferior; untilMs < 0 =
// sem limite superior. Serve para recortar a transcri\u00e7\u00e3o ao trecho da prega\u00e7\u00e3o sem
// perder o alinhamento com o v\u00eddeo original.
func LimparJanela(raw string, fromMs, untilMs int) (lines []string, blocks int) {
	// Normaliza quebras de linha e remove BOM, se houver.
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	raw = strings.TrimPrefix(raw, "\ufeff")

	rawBlocks := reBlocos.Split(raw, -1)

	lastMs := -1
	for _, b := range rawBlocks {
		b = strings.TrimSpace(b)
		if b == "" {
			continue
		}
		blockLines := strings.Split(b, "\n")

		// Acha a linha do intervalo de tempo (a que contém "-->").
		tIdx := -1
		for i, l := range blockLines {
			if strings.Contains(l, "-->") {
				tIdx = i
				break
			}
		}
		if tIdx == -1 {
			continue // bloco sem timecode: ignora
		}
		blocks++

		startMs, ok := parseStartMs(blockLines[tIdx])
		if !ok {
			continue
		}
		// Corte opcional: descarta tudo a partir de -until.
		if untilMs >= 0 && startMs >= untilMs {
			break
		}
		// Corte opcional inferior: pula falas antes de -from (sem mexer no lastMs).
		if fromMs >= 0 && startMs < fromMs {
			continue
		}
		// Garante que o início nunca retrocede (resolve sobreposição de tempo).
		if startMs < lastMs {
			startMs = lastMs
		}
		lastMs = startMs

		// O texto é tudo que vem DEPOIS da linha de tempo.
		text := CleanText(strings.Join(blockLines[tIdx+1:], " "))
		if text == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("[%s] %s", FormatMs(startMs), text))
	}
	return lines, blocks
}

// LimparArquivo lê o SRT em inPath, limpa e grava a transcrição em outPath.
// Devolve quantos blocos foram lidos e quantas linhas foram escritas.
func LimparArquivo(inPath, outPath string, untilMs int) (blocks, linhas int, err error) {
	raw, err := os.ReadFile(inPath)
	if err != nil {
		return 0, 0, err
	}
	lines, blocks := Limpar(string(raw), untilMs)
	if err := escrever(outPath, lines); err != nil {
		return blocks, 0, err
	}
	return blocks, len(lines), nil
}

// LimparArquivoJanela é como LimparArquivo, restrito à janela [fromMs, untilMs).
func LimparArquivoJanela(inPath, outPath string, fromMs, untilMs int) (blocks, linhas int, err error) {
	raw, err := os.ReadFile(inPath)
	if err != nil {
		return 0, 0, err
	}
	lines, blocks := LimparJanela(string(raw), fromMs, untilMs)
	if err := escrever(outPath, lines); err != nil {
		return blocks, 0, err
	}
	return blocks, len(lines), nil
}

// parseStartMs extrai o tempo de início ("HH:MM:SS,mmm --> ...") em milissegundos.
func parseStartMs(timeline string) (int, bool) {
	parts := strings.SplitN(timeline, "-->", 2)
	return HmsToMs(strings.TrimSpace(parts[0]))
}

// HmsToMs converte "HH:MM:SS,mmm" ou "HH:MM:SS.mmm" (ou sem ms) em milissegundos.
func HmsToMs(s string) (int, bool) {
	s = strings.TrimSpace(s)
	ms := 0
	if i := strings.IndexAny(s, ",."); i != -1 {
		frac := s[i+1:]
		s = s[:i]
		// normaliza a fração para 3 dígitos
		for len(frac) < 3 {
			frac += "0"
		}
		frac = frac[:3]
		var f int
		if _, err := fmt.Sscanf(frac, "%d", &f); err == nil {
			ms = f
		}
	}
	var h, m, sec int
	if _, err := fmt.Sscanf(s, "%d:%d:%d", &h, &m, &sec); err != nil {
		return 0, false
	}
	return ((h*60+m)*60+sec)*1000 + ms, true
}

// FormatMs devolve "HH:MM:SS" (sem milissegundos) para leitura humana.
func FormatMs(ms int) string {
	total := ms / 1000
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

// CleanText remove marcação (tags e anotações) e normaliza espaços, sem tocar nas palavras.
func CleanText(t string) string {
	t = reTagAngle.ReplaceAllString(t, "")
	t = reTagBrace.ReplaceAllString(t, "")
	t = reAnnotation.ReplaceAllString(t, "") // [Música], [Aplausos] etc. não são fala
	t = reSpaces.ReplaceAllString(t, " ")
	return strings.TrimSpace(t)
}

func escrever(path string, lines []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, l := range lines {
		if _, err := fmt.Fprintln(w, l); err != nil {
			return err
		}
	}
	return w.Flush()
}
