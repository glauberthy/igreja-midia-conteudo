package video

// PAUSAS DE FALA — fronteiras vindas do ÁUDIO, não da legenda.
//
// # Por que existe
//
// O corte terminava na fronteira do BLOCO DE LEGENDA seguinte, e os blocos da legenda rolling
// quebram por largura de tela, não por fim de fala. Medido no culto fZGyLBofmmo (2026-07-30):
//
//	transcrição: [01:30:05] "capaz de arrumar o caos. O Senhor traga"
//	             [01:30:07] "ordem na minha vida. Traga vida para…"
//	áudio:       fala corrida de 01:30:04.126 até 01:30:09.486
//
// O operador clicou na frase, o sistema terminou o corte em 01:30:06 — no MEIO da fala, 3,5 s
// antes da pausa. Todos os deltas do histórico de ações deram zero: o sistema aplicou fielmente
// um ponto que a LEGENDA inventou. Com a fronteira vinda do áudio, o mesmo clique cai em
// 01:30:09.486, que é onde a frase de fato termina.
//
// # Os parâmetros são de DESENHO, e foram medidos
//
// `d` (duração mínima da pausa) é o que decide, e o critério foi: qual limiar NÃO inventa pausa
// dentro de uma fala corrida conhecida? Medido na fala de 5,4 s acima:
//
//	d=0,10s  cinco pausas espúrias (136, 152, 100, 101, 108 ms) — micro-vãos entre palavras.
//	         Uma delas cai em 5406651, quase exatamente no corte errado que o operador fez.
//	d=0,15s  uma espúria (152 ms), logo depois do início da frase
//	d=0,20s  só as duas fronteiras REAIS
//	d=0,30s  só as duas fronteiras REAIS  <- escolhido, com margem
//
// `noise` (limiar em dB) quase não importa: no culto inteiro, de -30 a -40 dB o número de pausas
// vai de 2940 a 2026 com d=0,15 — a sala é silenciosa e a fala é alta, então a escolha cai numa
// região plana. -32 dB fica no meio dela.
//
// # O que estes parâmetros NÃO fazem
//
// Não distinguem "respirou" de "terminou a frase". A distribuição das 2736 pausas do culto é
// UNIMODAL (mediana 0,47 s; p25 0,24; p75 0,85; sem vale entre modas), e há contraexemplos
// diretos: uma pausa de 467 ms é fim de sentença ("…arrumar o caos.") e uma de 934 ms é meio de
// sentença ("…de tantas vozes que nunca me | trouxeram nada…"). Ou seja: a duração é PISTA de
// ordenação, não classificador. O limiar remove micro-vão entre palavras; escolher entre pausas
// candidatas continua sendo do operador — e é por isso que a régua as desenha.
//
// Por isso a duração de cada pausa é DEVOLVIDA: quem desenha pode diferenciar respiração curta
// de pausa longa, sem que o código finja saber qual é fim de sentença.

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
)

// Pausa é um intervalo de silêncio no áudio, em ms absolutos do vídeo.
type Pausa struct {
	InicioMs int `json:"inicio_ms"` // instante em que a fala PAROU
	FimMs    int `json:"fim_ms"`    // instante em que a fala VOLTOU
}

// DuracaoMs é o tamanho da pausa.
func (p Pausa) DuracaoMs() int { return p.FimMs - p.InicioMs }

// Padrões medidos (ver o comentário do pacote).
const (
	PausaNoiseDBPadrao = -32
	PausaMinMsPadrao   = 300
)

// OpcoesPausas parametriza a detecção. Zero usa os padrões medidos.
//
// Configurável porque o culto seguinte pode ter sala mais barulhenta ou pregador mais pausado, e
// afinar isso não pode exigir recompilar. Os valores usados são GRAVADOS junto do resultado —
// sem isso, uma régua desenhada com um limiar e um encaixe calculado com outro discordariam na
// tela, e o operador perderia a confiança nos dois.
type OpcoesPausas struct {
	NoiseDB int // limiar de silêncio em dB (negativo)
	MinMs   int // duração mínima para contar como pausa
}

// ComPadroes preenche o que estiver zerado com os padrões medidos.
func (o OpcoesPausas) ComPadroes() OpcoesPausas {
	if o.NoiseDB == 0 {
		o.NoiseDB = PausaNoiseDBPadrao
	}
	if o.MinMs <= 0 {
		o.MinMs = PausaMinMsPadrao
	}
	return o
}

var (
	reSilInicio = regexp.MustCompile(`silence_start:\s*(-?[\d.]+)`)
	reSilFim    = regexp.MustCompile(`silence_end:\s*(-?[\d.]+)`)
)

// DetectarPausas roda UMA passada de silencedetect sobre o áudio do vídeo e devolve as pausas em
// ordem cronológica.
//
// Custo medido: 6,5 s para 1h50 de áudio (arquivo de 902 MB), praticamente independente dos
// parâmetros — o que domina é decodificar o áudio. Feito uma vez por culto, guardado no cache.
//
// O vídeo NÃO é decodificado (-vn): só o áudio.
func DetectarPausas(ctx context.Context, exec Executor, bin, videoPath string, o OpcoesPausas) ([]Pausa, error) {
	o = o.ComPadroes()
	if bin == "" {
		bin = "ffmpeg"
	}
	args := []string{
		"-hide_banner", "-nostdin", "-i", videoPath, "-vn",
		"-af", fmt.Sprintf("silencedetect=noise=%ddB:d=%.3f", o.NoiseDB, float64(o.MinMs)/1000),
		"-f", "null", "-",
	}
	// O silencedetect escreve no STDERR (é log de filtro, não saída de mídia).
	_, saida, err := exec.Rodar(ctx, bin, args...)
	if err != nil {
		return nil, fmt.Errorf("detectando pausas em %s: %w (%s)", videoPath, err, resumoStderr(saida))
	}
	return parsearPausas(string(saida)), nil
}

// parsearPausas lê a saída do filtro. Um silence_start sem o silence_end correspondente (o
// arquivo acabou em silêncio) é DESCARTADO: pausa sem fim não é fronteira utilizável para corte.
func parsearPausas(saida string) []Pausa {
	var out []Pausa
	var aberta = -1
	for _, linha := range dividirLinhas(saida) {
		if m := reSilInicio.FindStringSubmatch(linha); m != nil {
			aberta = segParaMs(m[1])
			continue
		}
		if m := reSilFim.FindStringSubmatch(linha); m != nil && aberta >= 0 {
			fim := segParaMs(m[1])
			if fim > aberta {
				out = append(out, Pausa{InicioMs: aberta, FimMs: fim})
			}
			aberta = -1
		}
	}
	return out
}

func segParaMs(s string) int {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 {
		return 0
	}
	return int(f*1000 + 0.5)
}

func dividirLinhas(s string) []string {
	var out []string
	ini := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' || s[i] == '\r' {
			if i > ini {
				out = append(out, s[ini:i])
			}
			ini = i + 1
		}
	}
	if ini < len(s) {
		out = append(out, s[ini:])
	}
	return out
}

// ParsearPausasParaTeste expõe o parse da saída do silencedetect para teste.
//
// Exportada porque o teste vive no internal/servidor (é lá que o encaixe usa as pausas) e o parse
// é a fronteira com uma ferramenta externa: o número que sai dela vira CORTE. Testar por dentro
// do internal/video também serviria, mas duplicaria a fixture da saída do ffmpeg em dois pacotes.
func ParsearPausasParaTeste(saida string) []Pausa { return parsearPausas(saida) }
